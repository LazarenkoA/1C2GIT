package main

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	ConfigurationRepository "github.com/LazarenkoA/1C2GIT/Configuration"
	settings "github.com/LazarenkoA/1C2GIT/Confs"
	git "github.com/LazarenkoA/1C2GIT/Git"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"gopkg.in/mgo.v2/bson"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	di "go.uber.org/dig"
	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/mgo.v2"
)

type RotateConf struct{}
type msgtype byte

const (
	info msgtype = iota
	err

	ListenPort string = "2020"
)

////////////////////////////////////////////////////////////

type Hook struct {
}
type event func(rep *ConfigurationRepository.Notify)

func (h *Hook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.PanicLevel}
}
func (h *Hook) Fire(En *logrus.Entry) error {
	writeInfo(En.Message, "", "", time.Now(), err)
	return nil
}

const (
	limit int = 17
)

var (
	LogLevel           *int
	container          *di.Container
	logchan            chan map[string]interface{}
	mapUser            map[string]string
	kp                 *kingpin.Application
	eventsBeforeCommit []event
	eventsAfterCommit  []event
	logger             *logrus.Entry
)

func init() {
	// создаем контейнед DI
	container = di.New()

	logchan = make(chan map[string]interface{}, 10)
	mapUser = make(map[string]string)

	kp = kingpin.New("1C2GIT", "Приложение для синхронизации хранилища 1С и Git")
	LogLevel = kp.Flag("LogLevel", "Уровень логирования от 2 до 5\n"+
		"\t2 - ошибка\n"+
		"\t3 - предупреждение\n"+
		"\t4 - информация\n"+
		"\t5 - дебаг\n").
		Short('l').Default("3").Int()

	//flag.BoolVar(&help, "help", false, "Помощь")
}

func main() {
	kp.Parse(os.Args[1:])
	logrus.SetLevel(logrus.Level(2))
	logrus.AddHook(new(Hook))

	lw := new(logrusRotate.Rotate).Construct()
	defer lw.Start(*LogLevel, new(RotateConf))()
	logrus.SetFormatter(&logrus.JSONFormatter{})

	logger = logrusRotate.StandardLogger().WithField("name", "main")

	httpInitialise()
	initDIProvide()

	// для тестирования
	//go func() {
	//	timer := time.NewTicker(time.Second * 5)
	//	for t := range timer.C {
	//		writeInfo(fmt.Sprintf("test - %v", t.Second()), fake.FullName(), "", t, info)
	//	}
	//}()

	var sLoc *settings.Setting
	if err := container.Invoke(func(s *settings.Setting) {
		sLoc = s
	}); err != nil {
		logger.WithError(err).Panic("не удалось прочитать настройки")
	}
	initEvents()

	rep := new(ConfigurationRepository.Repository).New(sLoc.Bin1C)
	wg := new(sync.WaitGroup)
	mu := new(sync.Mutex)

	for _, r := range sLoc.RepositoryConf {
		logger.WithField("repository", r.GetRepPath()).Info("запуск отслеживания изменений по репозиторию")

		wg.Add(1)
		go rep.Observe(r, wg, func(n *ConfigurationRepository.Notify, rep *ConfigurationRepository.Repository) error {
			return gitCommit(mu, rep, n)
		})
	}

	fmt.Printf("Запуск ОК. Уровень логирования - %d\n", *LogLevel)
	wg.Wait()
}

func gitCommit(mu *sync.Mutex, rep *ConfigurationRepository.Repository, notify *ConfigurationRepository.Notify) error {
	mu.Lock()
	defer mu.Unlock()

	outDir := notify.RepInfo.GetDir()
	git_ := new(git.Git).New(outDir, notify, mapUser)
	defer git_.Destroy()
	defer func() {
		for _, e := range eventsAfterCommit {
			e(notify)
		}
	}()

	if err := git_.ResetHard(notify.RepInfo.GetBranch()); err != nil {
		logger.WithError(err).WithField("branch", notify.RepInfo.GetBranch()).Error("произошла ошибка при выполнении ResetHard")
		return err // если ветку не смогли переключить, логируемся и выходим, инчаче мы не в ту ветку закоммитим
	}

	// Запоминаем версию конфигурации. Сделано это потому что версия инерементируется в файлах, а не в хранилище 1С, что бы не перезатиралось.
	// TODO: подумать как обыграть это в настройках, а-ля файлы исключения, для xml файлов можно прикрутить xpath, что бы сохранять значение определенных узлов (как раз наш случай с версией)
	//r.SaveVersion()
	// Очищаем каталог перед выгрузкой, это нужно на случай если удаляется какой-то объект
	os.RemoveAll(outDir)

	// Как вариант можно параллельно грузить версии в темп каталоги, потом только переносить и пушить
	if err := rep.DownloadConfFiles(notify.RepInfo, notify.Version); err != nil {
		logger.WithField("Выгружаемая версия", notify.Version).
			WithField("Репозиторий", notify.RepInfo.GetRepPath()).
			Error("Ошибка выгрузки файлов из хранилища")
		return err
	} else {
		for _, e := range eventsBeforeCommit {
			e(notify)
		}

		rep.RestoreVersion(notify) // заисываем версию перед коммитом
		if err := git_.CommitAndPush(notify.RepInfo.GetBranch()); err != nil {
			logger.WithError(err).Error("Ошибка при выполнении push & commit")
			return err
		}

		logger.Debug("Синхронизация выполнена")
		writeInfo(fmt.Sprintf("Синхронизация %v выполнена", notify.RepInfo.GetRepPath()), notify.Author, notify.Comment, time.Now(), info)
	}

	return nil
}

func initDIProvide() {
	currentDir, _ := os.Getwd()
	settings.ReadSettings(path.Join(currentDir, "Confs", "MapUsers.yaml"), &mapUser)

	container.Provide(func() *settings.Setting {
		s := new(settings.Setting)
		settings.ReadSettings(path.Join(currentDir, "Confs", "Config.yaml"), s)
		return s
	})
	container.Provide(func(s *settings.Setting) (*mgo.Database, error) {
		return connectToDB(s)
	})
	tmp := &[]map[string]interface{}{} // в контейнере храним ссылку на слайс, что бы не приходилось обновлять каждый раз значение в контейнере
	container.Provide(func() *[]map[string]interface{} {
		return tmp
	})
}

func httpInitialise() {
	go http.ListenAndServe(":"+ListenPort, nil)
	fmt.Printf("Слушаем порт http %v\n", ListenPort)

	currentDir, _ := os.Getwd()
	indexhtml := path.Join(currentDir, "html/index.html")
	if _, err := os.Stat(indexhtml); os.IsNotExist(err) {
		logger.WithField("Path", indexhtml).Error("Не найден index.html")
		return
	}

	tplFuncMap := make(template.FuncMap)
	tplFuncMap["join"] = func(data []int, separator string) string {
		tmp := make([]string, len(data))
		for i, v := range data {
			tmp[i] = strconv.Itoa(v)
		}
		return strings.Join(tmp, separator)
	}
	tmpl, err := template.New(path.Base(indexhtml)).Funcs(tplFuncMap).ParseFiles(indexhtml)
	if err != nil {
		logger.WithError(err).Error("Ошибка парсинга шаблона")
		panic(err)
	}
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		type tData struct {
			Log           []map[string]interface{}
			ChartData     map[string]int
			ChartDataYear map[string][]int
		}

		f := func(db *mgo.Database) error {
			var items []map[string]interface{}
			var monthitems []map[string]interface{}
			var yearitems []map[string]interface{}

			startMonth := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Local)
			startYear := time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.Local)

			// в монго фильтрация делается так {Time: {$gt: ISODate("2021-11-22")}} // для примера
			if err := getDataStartDate(db, startMonth, &monthitems); err != nil {
				return err
			}
			if err := getDataStartDate(db, startYear, &yearitems); err != nil {
				return err
			}

			logger.WithField("start time", startMonth).WithField("Получено данных", len(monthitems)).
				Debug("Запрашиваем данные из БД за текущий месяц")
			logger.WithField("start time", startYear).WithField("Получено данных", len(yearitems)).
				Debug("Запрашиваем данные из БД за год")

			chartData := map[string]int{}
			chartDataYear := map[string][]int{}
			for _, v := range monthitems {
				autor := strings.Trim(v["_id"].(map[string]interface{})["autor"].(string), " ")
				count := v["count"].(int)
				chartData[autor] += count // если в имени пользователя есть пробел или был ранее, то субд вернет 2 записи по одному пользователю
			}
			for _, v := range yearitems {
				autor := strings.Trim(v["_id"].(map[string]interface{})["autor"].(string), " ")
				month := v["_id"].(map[string]interface{})["month"].(int)
				count := v["count"].(int)

				if _, ok := chartDataYear[autor]; !ok {
					chartDataYear[autor] = make([]int, 12, 12)
				}

				chartDataYear[autor][month-1] += count
			}

			if err := db.C("items").Find(bson.M{"Time": bson.M{"$exists": true}}).Sort("-Time").Limit(limit).All(&items); err == nil {
				tmpl.Execute(w, tData{items, chartData, chartDataYear})
			} else {
				logger.WithError(err).Error("Ошибка получения данных из БД")
				return err
			}

			return nil
		}

		if err := container.Invoke(f); err != nil {
			container.Invoke(func(logBufer *[]map[string]interface{}) {
				tmpl.Execute(w, tData{*logBufer, map[string]int{}, map[string][]int{}})
			})
		}
	})

	// статический контент
	staticHandlerimg := http.StripPrefix(
		"/img/",
		http.FileServer(http.Dir("html/img")),
	)
	staticHandlercss := http.StripPrefix(
		"/css/",
		http.FileServer(http.Dir("html/css")),
	)
	staticHandlerscript := http.StripPrefix(
		"/script/",
		http.FileServer(http.Dir("html/script")),
	)
	http.Handle("/img/", staticHandlerimg)
	http.Handle("/css/", staticHandlercss)
	http.Handle("/script/", staticHandlerscript)

	// Пояснение:
	// эта горутина нужна что бы читать из канала до того пока не загрузится http страничка (notifications)
	// потому как только тогда стартует чтение из канала, а если не читать из канала, у нас все выполнение застопорится
	// Сделано так, что при выполнении обработчика страницы notifications через контекст останавливается горутина
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
	exit:
		for range logchan {
			select {
			case <-ctx.Done():
				break exit
			default:
				continue
			}
		}
	}()

	once := new(sync.Once)
	http.HandleFunc("/notifications", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.WithError(err).Warning("Ошибка обновления веб сокета")
			return
		}

		go sendNewMsgNotifications(ws)

		// что б не запускалось при каждой перезагрузки страницы
		once.Do(func() {
			cancel()
		})
	})
}

func getDataStartDate(db *mgo.Database, startDate time.Time, result interface{}) error {
	group := []bson.M{
		{"$match": bson.M{"Time": bson.M{"$gt": startDate, "$exists": true}}},
		{"$group": bson.M{
			"_id":   bson.M{"month": bson.M{"$month": "$Time"}, "autor": "$autor"},
			"count": bson.M{"$sum": 1},
		}},
		{"$sort": bson.M{"_id": 1}},
	}
	return db.C("items").Pipe(group).All(result)
}

func writeInfo(str, autor, comment string, datetime time.Time, t msgtype) {
	log.Println(str)

	data := map[string]interface{}{
		//"_id": bson.NewObjectId(),
		"msg":      str,
		"datetime": datetime.Format("02.01.2006 (15:04)"),
		"comment":  comment,
		"type":     t,
		"autor":    autor,
		"Time":     datetime,
	}

	if err := container.Invoke(func(db *mgo.Database) {
		// Ошибки в монго не добавляем, нет смысла
		if t != err {
			db.C("items").Insert(data)
		}
	}); err != nil {
		container.Invoke(func(logBufer *[]map[string]interface{}) {
			// нужно на первое место поставить элемент, массив ограничиваем limit записями
			if len(*logBufer) > 0 {
				*logBufer = append((*logBufer)[:0], append([]map[string]interface{}{data}, (*logBufer)[0:]...)...)
				*logBufer = (*logBufer)[:int(math.Min(float64(len(*logBufer)), float64(limit)))]
			} else {
				*logBufer = append(*logBufer, data)
			}
		})
	}

	logchan <- data
}

func sendNewMsgNotifications(client *websocket.Conn) {
	for Ldata := range logchan {
		w, err := client.NextWriter(websocket.TextMessage)
		if err != nil {
			logger.Warningf("Ошибка записи сокета: %v", err)
			break
		}

		data, _ := json.Marshal(Ldata)
		w.Write(data)
		w.Close()
	}
}

func GetHash(Str string) string {
	first := sha1.New()
	first.Write([]byte(Str))

	return fmt.Sprintf("%x", first.Sum(nil))
}

func connectToDB(s *settings.Setting) (*mgo.Database, error) {
	if s.Mongo == nil {
		return nil, errors.New("MongoDB not use")
	}
	logrusRotate.StandardLogger().Info("Подключаемся к MongoDB")
	if sess, err := mgo.Dial(s.Mongo.ConnectionString); err == nil {
		return sess.DB("1C2GIT"), nil
	} else {
		//logrusRotate.StandardLogger().WithError(err).Error("Ошибка подключения к MongoDB")
		fmt.Println("Ошибка подключения к MongoDB:", err)
		return nil, err
	}
}

func initEvents() {
	eventsBeforeCommit = []event{}
	eventsAfterCommit = []event{}
}

///////////////// RotateConf ////////////////////////////////////////////////////
func (w *RotateConf) LogDir() string {
	currentDir, _ := os.Getwd()
	return filepath.Join(currentDir, "Logs")
}
func (w *RotateConf) FormatDir() string {
	return "02.01.2006"
}
func (w *RotateConf) FormatFile() string {
	return "15"
}
func (w *RotateConf) TTLLogs() int {
	return 12
}
func (w *RotateConf) TimeRotate() int {
	return 1
}
