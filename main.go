package main

import (
	ConfigurationRepository "1C2GIT/Configuration"
	settings "1C2GIT/Confs"
	git "1C2GIT/Git"
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	xmlpath "gopkg.in/xmlpath.v2"
)

var mapUser map[string]string

type RepositoryConf struct {
	TimerMinute int `json:"TimerMinute"`
	From        *struct {
		Rep       string `json:"Rep"`
		Login     string `json:"Login"`
		Pass      string `json:"Pass"`
		Extension bool   `json:"Extension"`
	} `json:"From"`
	To *struct {
		RepDir string `json:"RepDir"`
		Branch string `json:"Branch"`
	} `json:"To"`
	version string // для хранения версии конфигурации
}

type setting struct {
	Bin1C          string            `json:"Bin1C"`
	RepositoryConf []*RepositoryConf `json:"RepositoryConf"`
}
type msgtype byte

const (
	info msgtype = iota
	err

	ListenPort string = "2020"
)

func (r *RepositoryConf) GetRepPath() string {
	return r.From.Rep
}

func (r *RepositoryConf) GetLogin() string {
	return r.From.Login
}

func (r *RepositoryConf) GetPass() string {
	return r.From.Pass
}

func (r *RepositoryConf) IsExtension() bool {
	return r.From.Extension
}

func (r *RepositoryConf) GetOutDir() string {
	return r.To.RepDir
}

////////////////////////////////////////////////////////////

type Hook struct {
}

func (h *Hook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.PanicLevel}
}
func (h *Hook) Fire(En *logrus.Entry) error {
	writeInfo(En.Message, "", err)
	return nil
}

var (
	LogLevel int
	logBufer []map[string]interface{}
	logchan  chan map[string]interface{}
)

func main() {
	defer inilogrus().Stop()
	defer DeleleEmptyFile(logrus.StandardLogger().Out.(*os.File))

	logchan = make(chan map[string]interface{}, 10)
	wg := new(sync.WaitGroup)
	mu := new(sync.Mutex)

	httpInitialise()

	s := new(setting)
	mapUser = make(map[string]string)
	settings.ReadSettings(path.Join("Confs", "Config.conf"), s)

	mapFile := path.Join("Confs", "MapUsers.conf")
	if _, err := os.Stat(mapFile); os.IsNotExist(err) {
		logrus.Warningf("Не найден файл сопоставления пользователей %v", mapFile)
	} else {
		settings.ReadSettings(mapFile, &mapUser)
	}

	rep := new(ConfigurationRepository.Repository).New()
	//defer rep.Destroy()
	rep.BinPath = s.Bin1C
	for _, r := range s.RepositoryConf {
		wg.Add(1)
		go start(wg, mu, r, rep)
	}

	fmt.Printf("Запуск ОК. Уровень логирования - %d\n", LogLevel)
	wg.Wait()
}

func httpInitialise() {
	go http.ListenAndServe(":"+ListenPort, nil)
	fmt.Printf("Слушаем порт http %v\n", ListenPort)

	tmpl := template.Must(template.ParseFiles("html/index.html"))
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl.Execute(w, logBufer)
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
	ctx, cansel := context.WithCancel(context.Background())
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
			logrus.Panic(err)
		}

		go sendNewMsgNotifications(ws)

		// что б не запускалось при каждой перезагрузки страницы
		once.Do(func() {
			cansel()
		})
	})
}

func writeInfo(str, autor string, t msgtype) {
	fmt.Println(str)

	data := map[string]interface{}{
		"msg":   str,
		"type":  t,
		"autor": autor,
	}

	// сохраняем 15 последних запесей
	if len(logBufer) == 15 {
		logBufer = logBufer[1 : len(logBufer)-1] // Удаляем первый элемент
	}
	// нужно на первое место поставить элемент
	logBufer = append([]map[string]interface{}{data}, logBufer...)
	logchan <- data
}

func start(wg *sync.WaitGroup, mu *sync.Mutex, r *RepositoryConf, rep *ConfigurationRepository.Repository) {
	defer wg.Done()

	if r.TimerMinute <= 0 {
		logrus.WithField("Репозиторий", r.From.Rep).Error("Пропущена настройка, не задан параметр TimerMinute")
		return
	}

	invoke := func(t time.Time) {
		if _, err := os.Stat(r.To.RepDir); os.IsNotExist(err) {
			logrus.Debugf("Создаем каталог %q", r.To.RepDir)
			if err := os.Mkdir(r.To.RepDir, os.ModeDir); err != nil {
				logrus.WithError(err).Errorf("Ошибка создания каталога %q", r.To.RepDir)
			}
		}
		lastVersion := GetLastVersion(r.GetRepPath())
		logrus.WithField("Хранилище 1С", r.GetRepPath()).
			WithField("Начальная ревизия", lastVersion).
			Debug("Старт выгрузки")

		err, report := rep.GetReport(r, lastVersion+1)
		if err != nil {
			logrus.WithField("Репозиторий", r.GetRepPath()).Errorf("Ошибка получения отчета по хранилищу %v", err)
			return
		}
		if len(report) == 0 {
			logrus.WithField("Репозиторий", r.GetRepPath()).Debug("Репозиторий пустой")
			return
		}

		//mu2 := new(sync.Mutex)
		for i, _report := range report {
			logrus.WithField("report iteration", i+1).Debugf("Хранилище 1С %q", r.GetRepPath())

			// анонимная функция исключительно из-за defer, аналог try - catch
			git := new(git.Git).New(r.GetOutDir(), _report, mapUser)

			// все же Lock нужен, вот если бы у нас расширения были по разным каталогам, тогда можно было бы параллелить, а так не получится, будут коллизии на командах гита
			mu.Lock()
			func() {
				defer git.Destroy()
				mu.Unlock()

				if err = git.ResetHard(r.To.Branch); err != nil {
					logrus.WithError(err).Errorf("Произошла ошибка при выполнении Pull ветки на %v", r.To.Branch)
					return // если ветку не смогли переключить, логируемся и выходим, инчаче мы не в ту ветку закоммитим
				}

				// Запоминаем версию конфигурации. Сделано это потому что версия инерементируется в файлах, а не в хранилище 1С, что бы не перезатиралось.
				// TODO: подумать как обыграть это в настройках, а-ля файлы исключения, для xml файлов можно прикрутить xpath, что бы сохранять значение определенных узлов (как раз наш случай с версией)
				r.saveVersion()
				// Очищаем каталог перед выгрузкой, это нужно на случай если удаляется какой-то объект
				os.RemoveAll(r.GetOutDir())

				if err := rep.DownloadConfFiles(r, _report.Version); err != nil {
					logrus.WithField("Выгружаемая версия", _report.Version).
						WithField("Репозиторий", r.GetRepPath()).
						Error("Ошибка выгрузки файлов из хранилища")
					return
				} else {
					r.restoreVersion() // восстанавливаем версию перед коммитом
					if err := git.CommitAndPush(r.To.Branch); err != nil {
						logrus.Errorf("Ошибка при выполнении push & commit: %v", err)
					}

					SeveLastVersion(r.GetRepPath(), _report.Version)
					logrus.WithField("Время", t).Debug("Синхронизация выполнена")
					writeInfo(fmt.Sprintf("Синхронизация %v выполнена. Время %v\n\r", r.GetRepPath(), t.Format("02.01.2006 (15:04)")), _report.Author, info)
				}
			}()

		}
	}

	logrus.WithField("Хранилище 1С", r.GetRepPath()).Debugf("Таймер по %d минут", r.TimerMinute)
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
			logrus.Errorf("Ошибка записи сокета: %v", err)
			break
		}

		data, _ := json.Marshal(Ldata)
		w.Write(data)
		w.Close()
	}
}

func (this *RepositoryConf) saveVersion() {
	logrus.WithField("Репозиторий", this.To.RepDir).WithField("Версия", this.version).Debug("Сохраняем версию расширения")

	ConfigurationFile := path.Join(this.To.RepDir, "Configuration.xml")
	if _, err := os.Stat(ConfigurationFile); os.IsNotExist(err) {
		logrus.WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Error("Конфигурационный файл (Configuration.xml) не найден")
		return
	}

	file, err := os.Open(ConfigurationFile)
	if err != nil {
		logrus.WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Errorf("Ошибка открытия: %q", err)
		return
	}
	defer file.Close()

	xmlroot, xmlerr := xmlpath.Parse(bufio.NewReader(file))
	if xmlerr != nil {
		logrus.WithField("Файл", ConfigurationFile).Errorf("Ошибка чтения xml: %q", xmlerr.Error())
		return
	}

	path := xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Version/text()")
	if value, ok := path.String(xmlroot); ok {
		this.version = value
	} else {
		// значит версии нет, установим начальную
		this.version = "1.0.0"
		logrus.WithField("Файл", ConfigurationFile).Debugf("В файле не было версии, установили %q", this.version)
	}

}

func (this *RepositoryConf) restoreVersion() {
	logrus.WithField("Репозиторий", this.To.RepDir).WithField("Версия", this.version).Debug("Восстанавливаем версию расширения")

	ConfigurationFile := path.Join(this.To.RepDir, "Configuration.xml")
	if _, err := os.Stat(ConfigurationFile); os.IsNotExist(err) {
		logrus.WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Error("Конфигурационный файл (Configuration.xml) не найден")
		return
	}

	// Меняем версию, без парсинга, поменять значение одного узла прям проблема, а повторять структуру xml в классе ой как не хочется
	// Читаем файл
	file, err := os.Open(ConfigurationFile)
	if err != nil {
		logrus.WithField("Файл", ConfigurationFile).Errorf("Ошибка открытия файла: %q", err)
		return
	}

	stat, _ := file.Stat()
	buf := make([]byte, stat.Size())
	if _, err = file.Read(buf); err != nil {
		logrus.WithField("Файл", ConfigurationFile).Errorf("Ошибка чтения файла: %q", err)
		return
	}
	file.Close()
	os.Remove(ConfigurationFile)

	xml := string(buf)
	reg := regexp.MustCompile(`(?i)(?:<Version>(.+?)<\/Version>|<Version\/>)`)
	xml = reg.ReplaceAllString(xml, "<Version>"+this.version+"</Version>")

	// сохраняем файл
	file, err = os.OpenFile(ConfigurationFile, os.O_CREATE, os.ModeExclusive)
	if err != nil {
		logrus.WithField("Файл", ConfigurationFile).Errorf("Ошибка создания: %q", err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(xml); err != nil {
		logrus.WithField("Файл", ConfigurationFile).Errorf("Ошибка записи: %q", err)
		return
	}
}

func inilogrus() *time.Ticker {
	flag.IntVar(&LogLevel, "LogLevel", 3, "Уровень логирования от 2 до 5, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг")

	flag.Parse()
	currentDir, _ := os.Getwd()

	createNewDir := func() string {
		dir := filepath.Join(currentDir, "Logs", time.Now().Format("02.01.2006"))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			os.MkdirAll(dir, os.ModePerm)
		}
		return dir
	}

	Log1, _ := os.OpenFile(filepath.Join(createNewDir(), "Log_"+time.Now().Format("15.04.05")), os.O_CREATE, os.ModeAppend)
	logrus.SetOutput(Log1)

	timer := time.NewTicker(time.Minute * 10)
	go func() {
		for range timer.C {
			Log, _ := os.OpenFile(filepath.Join(createNewDir(), "Log_"+time.Now().Format("15.04.05")), os.O_CREATE, os.ModeAppend)
			oldFile := logrus.StandardLogger().Out.(*os.File)
			logrus.SetOutput(Log)
			DeleleEmptyFile(oldFile)
		}
	}()

	logrus.SetLevel(logrus.Level(LogLevel))
	logrus.AddHook(new(Hook))

	//line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	//fmt.Println(line)

	return timer
}

func DeleleEmptyFile(file *os.File) {
	if file == nil {
		return
	}
	// Если файл пустой, удаляем его. что бы не плодил кучу файлов
	info, _ := file.Stat()
	if info.Size() == 0 && !info.IsDir() {
		file.Close()

		if err := os.Remove(file.Name()); err != nil {
			logrus.WithError(err).WithField("Файл", file.Name()).Error("Ошибка удаления файла")
		}
	}

	var dirPath string
	// Для каталога, если пустой, то зачем он нам
	if !info.IsDir() {
		dirPath, _ = filepath.Split(file.Name())
	} else {
		dirPath = file.Name()
		file.Close()
	}

	// Если в текущем каталоге нет файлов, пробуем удалить его
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		logrus.WithError(err).WithField("Каталог", dirPath).Error("Ошибка получения списка файлов в каталоге")
		return
	}

	if len(files) == 0 {
		os.Remove(dirPath)
	}

}

func GetHash(Str string) string {
	first := sha1.New()
	first.Write([]byte(Str))

	return fmt.Sprintf("%x", first.Sum(nil))
}

func GetLastVersion(RepPath string) int {
	logrus.WithField("Репозиторий", RepPath).Debug("Получаем последнюю версию коммита")

	currentDir, _ := os.Getwd()
	part := strings.Split(RepPath, "\\")
	if len(part) == 1 { // значит путь с разделителем "/"
		part = strings.Split(RepPath, "/")
	}
	filePath := filepath.Join(currentDir, part[len(part)-1])

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logrus.Debug("Файл " + filePath + " не найден")
		return 0
	}

	if errRead, versionStr := ConfigurationRepository.ReadFile(filePath, nil); errRead == nil {
		V := string(*versionStr)
		logrus.WithField("Репозиторий", RepPath).WithField("version", V).Debug("Получили последнюю версию коммита")

		if version, err := strconv.Atoi(V); err != nil {
			logrus.Error("Версия не является числом \"" + V + "\"")
			return 1
		} else {
			return version
		}
	} else {
		logrus.WithField("Файл", filePath).Errorf("Ошибка чтения файла: \n %v", errRead)
		return 1
	}
}

func SeveLastVersion(RepPath string, Version int) {
	logrus.WithField("Репозиторий", RepPath).WithField("Версия", Version).Debug("Обновляем последнюю версию коммита")

	currentDir, _ := os.Getwd()
	part := strings.Split(RepPath, "\\")
	if len(part) == 1 { // значит путь с разделителем "/"
		part = strings.Split(RepPath, "/")
	}
	filePath := filepath.Join(currentDir, part[len(part)-1])

	err := ioutil.WriteFile(filePath, []byte(fmt.Sprint(Version)), os.ModeExclusive)
	if err != nil {
		logrus.WithField("файл", filePath).WithField("Ошибка", err).Error("Ошибка записи файла")
	}
}
