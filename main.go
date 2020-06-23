package main

import (
	ConfigurationRepository "1C2GIT/Configuration"
	settings "1C2GIT/Confs"
	git "1C2GIT/Git"
	"Teaching/github.com/pkg/errors"
	"context"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/yaml.v2"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	di "go.uber.org/dig"
	mgo "gopkg.in/mgo.v2"
)

var mapUser map[string]string
type RotateConf struct {}
type msgtype byte

const (
	info msgtype = iota
	err

	ListenPort string = "2020"
)

////////////////////////////////////////////////////////////

type Hook struct {
}

func (h *Hook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.PanicLevel}
}
func (h *Hook) Fire(En *logrus.Entry) error {
	writeInfo(En.Message, "", "", time.Now(), err)
	return nil
}

var (
	LogLevel int
	limit int = 15
	container *di.Container
	logchan chan map[string]interface{}
)

func main() {
	flag.IntVar(&LogLevel, "LogLevel", 3, "Уровень логирования от 2 до 5, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг")
	flag.Parse()
	logrus.SetLevel(logrus.Level(2))
	logrus.AddHook(new(Hook))

	lw := new(logrusRotate.Rotate).Construct()
	defer lw.Start(LogLevel, new(RotateConf))()

	logchan = make(chan map[string]interface{}, 10)
	wg := new(sync.WaitGroup)
	mu := new(sync.Mutex)

	httpInitialise()


	mapUser = make(map[string]string)
	currentDir, _ := os.Getwd()
	settings.ReadSettings(path.Join(currentDir, "Confs", "MapUsers.conf"), &mapUser)

	// создаем контейнед DI
	container = di.New()
	container.Provide(func() *settings.Setting {
		s := new(settings.Setting)
		settings.ReadSettings(path.Join(currentDir, "Confs", "Config.conf"), s)
		return s
	})
	container.Provide(func(s *settings.Setting) (*mgo.Database, error) {
		return connectToDB(s)
	})
	tmp := &[]map[string]interface{}{} // в контейнере храним ссылку на слайс, что бы не приходилось обновлять каждый раз значение в контейнере
	container.Provide(func() *[]map[string]interface{} {
		return tmp
	})


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
		logrusRotate.StandardLogger().WithError(err).Panic("Не удалось прочитать настройки")
	}

	rep := new(ConfigurationRepository.Repository).New()
	//defer rep.Destroy()
	rep.BinPath = sLoc.Bin1C
	for _, r := range sLoc.RepositoryConf {
		wg.Add(1)
		go start(wg, mu, r, rep)
	}

	fmt.Printf("Запуск ОК. Уровень логирования - %d\n", LogLevel)
	wg.Wait()
}

func httpInitialise() {
	go http.ListenAndServe(":"+ListenPort, nil)
	fmt.Printf("Слушаем порт http %v\n", ListenPort)

	currentDir, _ := os.Getwd()
	tmpl := template.Must(template.ParseFiles(path.Join(currentDir, "html/index.html")))
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := container.Invoke(func(db *mgo.Database) {
			var items []map[string]interface{}
			var monthitems []map[string]interface{}

			startMonth := time.Now().AddDate(0,0, -time.Now().Day())
			db.C("items").Find(bson.M{
				"Time": bson.M{"$gt": startMonth, "$exists": true},
			}).All(&monthitems)

			// группируем по автору
			monthitemsGroup := map[string]int{}
			for _, v := range monthitems {
				monthitemsGroup[v["autor"].(string)]++
			}

			chartData := []map[string]interface{}{}
			for k, v := range monthitemsGroup {
				chartData = append(chartData, map[string]interface{}{"Name": k, "Count": v})
			}

			// bson.M{} - это типа условия для поиска
			if err := db.C("items").Find(bson.M{"Time": bson.M{"$exists": true}}).Sort("-Time").Limit(limit).All(&items); err == nil {
				tmpl.Execute(w,  struct {
					Log[]map[string]interface{}
					СhartData[]map[string]interface{}
				}{items,  chartData} )
			} else {
				logrusRotate.StandardLogger().WithError(err).Error("Ошибка получения данных из БД")
			}
		}); err != nil {
			container.Invoke(func(logBufer *[]map[string]interface{}) {
				tmpl.Execute(w, struct {
					Log []map[string]interface{}
					СhartData[]map[string]interface{}
				}{*logBufer, []map[string]interface{}{}})
			})
		}
	})

	// статический контент
	staticHandler := http.StripPrefix(
		"/img/",
		http.FileServer(http.Dir("html/img")),
	)
	http.Handle("/img/", staticHandler)

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
			logrusRotate.StandardLogger().WithError(err).Warning("Ошибка обновления веб сокета")
			return
		}

		go sendNewMsgNotifications(ws)

		// что б не запускалось при каждой перезагрузки страницы
		once.Do(func() {
			cancel()
		})
	})
}

func writeInfo(str, autor, comment string, datetime time.Time, t msgtype) {
	log.Println(str)

	data := map[string]interface{}{
		//"_id": bson.NewObjectId(),
		"msg":   str,
		"datetime": datetime.Format("02.01.2006 (15:04)"),
		"comment":   comment,
		"type":  t,
		"autor": autor,
		"Time" : datetime,
	}

	if err := container.Invoke(func(db *mgo.Database) {
		db.C("items").Insert(data)
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

func start(wg *sync.WaitGroup, mu *sync.Mutex, r *settings.RepositoryConf, rep *ConfigurationRepository.Repository) {
	defer wg.Done()

	if r.TimerMinute <= 0 {
		logrusRotate.StandardLogger().WithField("Репозиторий", r.From.Rep).Error("Пропущена настройка, не задан параметр TimerMinute")
		return
	}

	invoke := func(t time.Time) {
		vInfo := make(map[string]int, 0)

		if _, err := os.Stat(r.To.RepDir); os.IsNotExist(err) {
			logrusRotate.StandardLogger().Debugf("Создаем каталог %q", r.To.RepDir)
			if err := os.Mkdir(r.To.RepDir, os.ModeDir); err != nil {
				logrusRotate.StandardLogger().WithError(err).Errorf("Ошибка создания каталога %q", r.To.RepDir)
			}
		}
		GetLastVersion(vInfo)


		logrusRotate.StandardLogger().WithField("Хранилище 1С", r.GetRepPath()).
			WithField("Начальная ревизия", vInfo[r.GetRepPath()]).
			Debug("Старт выгрузки")

		err, report := rep.GetReport(r, vInfo[r.GetRepPath()]+1)
		if err != nil {
			logrusRotate.StandardLogger().WithField("Репозиторий", r.GetRepPath()).Errorf("Ошибка получения отчета по хранилищу %v", err)
			return
		}
		if len(report) == 0 {
			logrusRotate.StandardLogger().WithField("Репозиторий", r.GetRepPath()).Debug("Репозиторий пустой")
			return
		}

		//mu2 := new(sync.Mutex)
		for i, _report := range report {
			logrusRotate.StandardLogger().WithField("report iteration", i+1).Debugf("Хранилище 1С %q", r.GetRepPath())
			// все же Lock нужен, вот если бы у нас расширения были по разным каталогам, тогда можно было бы параллелить, а так не получится, будут коллизии на командах гита
			mu.Lock()

			// анонимная функция исключительно из-за defer, аналог try - catch
			git := new(git.Git).New(r.GetOutDir(), _report, mapUser)
			func() {
				defer mu.Unlock()
				defer git.Destroy()

				if err = git.ResetHard(r.To.Branch); err != nil {
					logrusRotate.StandardLogger().WithError(err).Errorf("Произошла ошибка при выполнении Pull ветки на %v", r.To.Branch)
					return // если ветку не смогли переключить, логируемся и выходим, инчаче мы не в ту ветку закоммитим
				}

				// Запоминаем версию конфигурации. Сделано это потому что версия инерементируется в файлах, а не в хранилище 1С, что бы не перезатиралось.
				// TODO: подумать как обыграть это в настройках, а-ля файлы исключения, для xml файлов можно прикрутить xpath, что бы сохранять значение определенных узлов (как раз наш случай с версией)
				r.SaveVersion()
				// Очищаем каталог перед выгрузкой, это нужно на случай если удаляется какой-то объект
				os.RemoveAll(r.GetOutDir())

				// Как вариант можно параллельно грузить версии в темп каталоги, потом только переносить и пушить
				if err := rep.DownloadConfFiles(r, _report.Version); err != nil {
					logrusRotate.StandardLogger().WithField("Выгружаемая версия", _report.Version).
						WithField("Репозиторий", r.GetRepPath()).
						Error("Ошибка выгрузки файлов из хранилища")
					return
				} else {
					r.RestoreVersion() // восстанавливаем версию перед коммитом
					if err := git.CommitAndPush(r.To.Branch); err != nil {
						logrusRotate.StandardLogger().Errorf("Ошибка при выполнении push & commit: %v", err)
						return
					}

					vInfo[r.GetRepPath()] = _report.Version
					SeveLastVersion(vInfo)
					logrusRotate.StandardLogger().WithField("Время", t).Debug("Синхронизация выполнена")
					writeInfo(fmt.Sprintf("Синхронизация %v выполнена", r.GetRepPath()), _report.Author, _report.Comment, t, info)
				}
			}()

		}
	}

	logrusRotate.StandardLogger().WithField("Хранилище 1С", r.GetRepPath()).Debugf("Таймер по %d минут", r.TimerMinute)
	timer := time.NewTicker(time.Minute * time.Duration(r.TimerMinute))
	invoke(time.Now()) // первый раз при запуске, потом будет по таймеру. Сделано так, что бы не ждать наступления события при запуске
	for time := range timer.C {
		invoke(time)
	}
}

func sendNewMsgNotifications(client *websocket.Conn) {
	for Ldata := range logchan {
		w, err := client.NextWriter(websocket.TextMessage)
		if err != nil {
			logrusRotate.StandardLogger().Warningf("Ошибка записи сокета: %v", err)
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

func GetLastVersion(v map[string]int) {
	logrusRotate.StandardLogger().Debug("Получаем последнюю версию коммита")

	currentDir, _ := os.Getwd()
	filePath := filepath.Join(currentDir, "versions")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logrusRotate.StandardLogger().WithField("файл", filePath).Warning("Файл версий не найден")
		return
	}

	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", filePath).WithError(err).Warning("Ошибка открытия файла")
		return
	}

	err = yaml.Unmarshal(file, v)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", filePath).WithError(err).Warning("Ошибка чтения конфигурационного файла")
		return
	}
}

func SeveLastVersion(v map[string]int) {
	logrusRotate.StandardLogger().WithField("Данные версий", v).Debug("Обновляем последнюю версию коммита")

	currentDir, _ := os.Getwd()
	filePath := filepath.Join(currentDir, "versions")


	b, err := yaml.Marshal(v)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", filePath).WithError(err).Warning("Ошибка сериализации")
		return
	}
	if err = ioutil.WriteFile(filePath, b, os.ModeAppend|os.ModePerm); err != nil {
		logrusRotate.StandardLogger().WithField("файл", filePath).WithField("Ошибка", err).Error("Ошибка записи файла")
	}
}

func connectToDB(s *settings.Setting) (*mgo.Database, error)  {
	if s.Mongo == nil {
		return nil, errors.New("MongoDB not use")
	}
	logrusRotate.StandardLogger().Info("Подключаемся к MongoDB")
	if sess, err := mgo.Dial(s.Mongo.ConnectionString); err == nil {
		return sess.DB("1C2GIT"), nil
	} else {
		return  nil, err
	}
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
