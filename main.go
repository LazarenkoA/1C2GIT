package main

import (
	ConfigurationRepository "1C2GIT/Configuration"
	settings "1C2GIT/Confs"
	git "1C2GIT/Git"
	"crypto/sha1"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var mapUser map[string]string

type from struct {
	Rep       string `json:"Rep"`
	Login     string `json:"Login"`
	Pass      string `json:"Pass"`
	Extension bool   `json:"Extension"`
}

type to struct {
	RepDir string `json:"RepDir"`
	Branch string `json:"Branch"`
}

type RepositoryConf struct {
	TimerMinute int   `json:"TimerMinute"`
	From        *from `json:"From"`
	To          *to   `json:"To"`
}

type setting struct {
	Bin1C          string            `json:"Bin1C"`
	RepositoryConf []*RepositoryConf `json:"RepositoryConf"`
}

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
	fmt.Println(En.Message)
	return nil
}

var (
	LogLevel int
)

func main() {
	defer inilogrus().Stop()
	defer DeleleEmptyFile(logrus.StandardLogger().Out.(*os.File))

	s := new(setting)
	mapUser = make(map[string]string, 0)
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

	mu := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for _, r := range s.RepositoryConf {
		wg.Add(1)
		go start(wg, mu, r, rep)
	}

	fmt.Printf("Запуск ОК. Уровень логирования - %d\n\r", LogLevel)
	wg.Wait()
}

func start(wg *sync.WaitGroup, mu *sync.Mutex, r *RepositoryConf, rep *ConfigurationRepository.Repository) {
	defer wg.Done()

	if r.TimerMinute <= 0 {
		logrus.WithField("Репозиторий", r.From.Rep).Error("Пропущена настройка, не задан параметр TimerMinute")
		return
	}

	logrus.WithField("Хранилище 1С", r.GetRepPath()).Debugf("Таймер по %d минут", r.TimerMinute)
	timer := time.NewTicker(time.Minute * time.Duration(r.TimerMinute))
	for time := range timer.C {
		lastVersion := GetLastVersion(r.GetRepPath())

		logrus.WithField("Хранилище 1С", r.GetRepPath()).
			WithField("Начальная версия", lastVersion).
			Debug("Старт выгрузки")

		err, report := rep.GetReport(r, r.GetOutDir(), lastVersion+1)
		if err != nil {
			logrus.WithField("Репозиторий", r.GetRepPath()).Errorf("Ошибка получения отчета по хранилищу %v", err)
			continue
		}
		if len(report) == 0 {
			continue
		}
		for _, _report := range report {
			// Очищаем каталог перед выгрузкой, это нужно на случай если удаляется какой-то объект
			os.RemoveAll(r.GetOutDir())

			if err := rep.DownloadConfFiles(r, _report.Version); err != nil {
				logrus.WithField("Выгружаемая версия", _report.Version).
					WithField("Репозиторий", r.GetRepPath()).
					Error("Ошибка выгрузки файлов из хранилища")
				break
			} else {
				// с гитом лучше не работать параллельно, там меняются переменные окружения
				mu.Lock()

				// т.к. нет try catch, извращаемся как можем
				func() {
					git := new(git.Git).New(r.GetOutDir(), _report, mapUser)
					defer git.Destroy()

					if err := git.CommitAndPush(r.To.Branch); err != nil {
						logrus.Errorf("Ошибка при выполнении Push: %v", err)
					}
				}()

				mu.Unlock()
				SeveLastVersion(r.GetRepPath(), _report.Version)
			}

		}

		logrus.WithField("Время", time).Debug("Синхронизация выполнена")
		fmt.Printf("Синхронизация %v выполнена. Время %v\n\r", r.GetRepPath(), time)
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
	// Если файл пустой, удаляем его. что бы не плодил кучу файлов
	info, _ := file.Stat()
	if info.Size() == 0 {
		file.Close()

		if err := os.Remove(file.Name()); err != nil {
			logrus.WithError(err).WithField("Файл", file.Name()).Error("Ошибка удаления пустого файла логов")
		}
	}
}

func GetHash(Str string) string {
	first := sha1.New()
	first.Write([]byte(Str))

	return fmt.Sprintf("%x", first.Sum(nil))
}

func GetLastVersion(RepPath string) int {
	currentDir, _ := os.Getwd()
	part := strings.Split(RepPath, "\\")
	if len(part) == 1 { // значит путь с разделителем "/"
		part = strings.Split(RepPath, "/")
	}
	filePath := filepath.Join(currentDir, part[len(part)-1])

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logrus.Debug("Файл " + filePath + " не найден")
		return 1
	}

	if errRead, versionStr := ConfigurationRepository.ReadFile(filePath, nil); errRead == nil {
		V := string(*versionStr)
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
