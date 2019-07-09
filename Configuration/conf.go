package ConfigurationRepository

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

type Repository struct {
	BinPath string
	//tmpDBPath string
}

type IRepository interface {
	GetRepPath() string
	GetLogin() string
	GetPass() string
	IsExtension() bool
	GetOutDir() string
}

type RepositoryInfo struct {
	Version int
	Author  string
	Date    time.Time
	Comment string
}

func (r *RepositoryInfo) GetComment() string {
	return r.Comment
}

func (r *RepositoryInfo) GetAuthor() string {
	return r.Author
}

func (r *RepositoryInfo) GetDateTime() *time.Time {
	return &r.Date
}

func (this *Repository) New() *Repository {
	//this.uuid, _ = uuid.NewV4()
	return this
}

func (this *Repository) createTmpFile() string {
	fileLog, err := ioutil.TempFile("", "OutLog_")

	defer fileLog.Close() // Закрываем иначе в него 1С не сможет записать

	if err != nil {
		panic(fmt.Errorf("Ошибка получения временного файла:\n %v", err))
	}
	return fileLog.Name()
}

// CreateTmpBD метод создает временную базу данных
func (this *Repository) createTmpBD(createExtension bool) (error, string) {
	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")

	defer func() {
		if er := recover(); er != nil {
			logrus.Error(fmt.Errorf("Произошла ошибка при создании временной базы: %v", er))
			os.RemoveAll(tmpDBPath)
		}
	}()

	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)
	cmd := exec.Command(this.BinPath, "CREATEINFOBASE", fmt.Sprintf("File=%v", tmpDBPath), fmt.Sprintf("/OUT  %v", fileLog))

	if err := this.run(cmd, fileLog); err != nil {
		logrus.Panic(err)
	}

	if createExtension {
		currentDir, _ := os.Getwd()
		Ext := filepath.Join(currentDir, "tmp.cfe")

		if _, err := os.Stat(Ext); os.IsNotExist(err) {
			return fmt.Errorf("В каталоге с программой не найден файл расширения tmp.cfe"), tmpDBPath
		}

		param := []string{}
		param = append(param, "DESIGNER")
		param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
		param = append(param, fmt.Sprintf("/LoadCfg %v", Ext))
		param = append(param, "-Extension temp")
		param = append(param, fmt.Sprintf("/OUT  %v", fileLog))
		cmd := exec.Command(this.BinPath, param...)

		if err := this.run(cmd, fileLog); err != nil {
			logrus.WithError(err).Panic("Ошибка загрузки расширения в базу.")
		}
	}

	return nil, tmpDBPath
}

func (this *Repository) DownloadConfFiles(DataRep IRepository, version int) (err error) {
	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("Произошла ошибка при сохранении конфигурации конфигурации в файлы: %v", er)
		}
	}()

	logrus.Debug("Сохраняем конфигурацию в файлы")

	var tmpDBPath string
	if err, tmpDBPath = this.createTmpBD(DataRep.IsExtension()); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDBPath)

	// ПОДКЛЮЧАЕМ к ХРАНИЛИЩУ и ОБНОВЛЯЕМ ДО ОПРЕДЕЛЕННОЙ ВЕРСИИ
	this.ConfigurationRepositoryBindCfg(DataRep, tmpDBPath, version)

	// ОБНОВЛЯЕМ ДО ОПРЕДЕЛЕННОЙ ВЕРСИИ
	//rep.ConfigurationRepositoryUpdateCfg(DataRep, tmpDBPath, version)

	// СОХРАНЯЕМ В ФАЙЛЫ
	this.DumpConfigToFiles(DataRep, tmpDBPath)

	return nil
}

func (this *Repository) ConfigurationRepositoryBindCfg(DataRep IRepository, fileDBPath string, version int) {
	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, fmt.Sprintf("/F %v", fileDBPath))
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", DataRep.GetRepPath()))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryN %v", DataRep.GetLogin()))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryP %v", DataRep.GetPass()))
	param = append(param, "/ConfigurationRepositoryBindCfg")
	param = append(param, "-forceBindAlreadyBindedUser")
	param = append(param, "-forceReplaceCfg")
	if DataRep.IsExtension() {
		param = append(param, "-Extension temp")
	}
	param = append(param, "/ConfigurationRepositoryUpdateCfg")
	param = append(param, fmt.Sprintf("-v %d", version))
	param = append(param, "-force")
	param = append(param, "-revised")
	if DataRep.IsExtension() {
		param = append(param, "-Extension temp")
	}

	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	if err := this.run(exec.Command(this.BinPath, param...), fileLog); err != nil {
		logrus.Panic(err)
	}
}

func (this *Repository) ConfigurationRepositoryUpdateCfg(DataRep IRepository, fileDBPath string, version int) {
	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, fmt.Sprintf("/F %v", fileDBPath))
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, "/ConfigurationRepositoryUpdateCfg")
	param = append(param, fmt.Sprintf("-v %d", version))
	param = append(param, "-force")
	param = append(param, "-revised")
	if DataRep.IsExtension() {
		param = append(param, "-Extension temp")
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	if err := this.run(exec.Command(this.BinPath, param...), fileLog); err != nil {
		logrus.Panic(err)
	}
}

func (this *Repository) DumpConfigToFiles(DataRep IRepository, fileDBPath string) {
	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, fmt.Sprintf("/F %v", fileDBPath))
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/DumpConfigToFiles %v", DataRep.GetOutDir()))
	if DataRep.IsExtension() {
		param = append(param, "-Extension temp")
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	if err := this.run(exec.Command(this.BinPath, param...), fileLog); err != nil {
		logrus.Panic(err)
	}
}

func (this *Repository) GetReport(DataRep IRepository, DitOut string, version int) (error, []*RepositoryInfo) {
	result := []*RepositoryInfo{}

	report := this.saveReport(DataRep, version)
	if report == "" {
		return fmt.Errorf("Не удалось получить отчет по хранилищу"), result
	}

	// Двойные кавычки в комментарии мешают, по этому мы заменяем из на одинарные
	report = strings.Replace(report, "\"\"", "'", -1)

	tmpArray := [][]string{}
	reg := regexp.MustCompile(`[\{]"#","([^"]+)["][\}]`)
	matches := reg.FindAllStringSubmatch(report, -1)
	for _, s := range matches {
		if s[1] == "Версия:" {
			tmpArray = append(tmpArray, []string{})
		}

		if len(tmpArray) > 0 {
			tmpArray[len(tmpArray)-1] = append(tmpArray[len(tmpArray)-1], s[1])
		}
	}

	for _, array := range tmpArray {
		RepInfo := new(RepositoryInfo)
		for id, s := range array {
			switch s {
			case "Версия:":
				if version, err := strconv.Atoi(array[id+1]); err == nil {
					RepInfo.Version = version
				}
			case "Пользователь:":
				RepInfo.Author = array[id+1]
			case "Комментарий:":
				// Комментария может не быть, по этому вот такой костыльчик
				if array[id+1] != "Изменены:" {
					RepInfo.Comment = strings.Replace(array[id+1], "\n", " ", -1)
					RepInfo.Comment = strings.Replace(array[id+1], "\r", "", -1)
				}
			case "Дата создания:":
				if t, err := time.Parse("02.01.2006", array[id+1]); err == nil {
					RepInfo.Date = t
				}
			case "Время создания:":
				if !RepInfo.Date.IsZero() {
					str := RepInfo.Date.Format("02.01.2006") + " " + array[id+1]
					if t, err := time.Parse("02.01.2006 15:04:05", str); err == nil {
						RepInfo.Date = t
					}
				}
			}
		}
		RepInfo.Comment = fmt.Sprintf("Хранилище: %v\n"+
			"Версия: %v\n"+
			"Коментарий: %q", DataRep.GetRepPath(), RepInfo.Version, RepInfo.Comment)
		result = append(result, RepInfo)
	}

	return nil, result
}

func (this *Repository) saveReport(DataRep IRepository, versionStart int) string {
	defer func() {
		if er := recover(); er != nil {
			logrus.Error(fmt.Errorf("Произошла ошибка при получении истории из хранилища: %v", er))
		}
	}()

	logrus.Debug("Сохраняем отчет конфигурации в файл")

	fileLog := this.createTmpFile()
	fileResult := this.createTmpFile()
	defer os.Remove(fileLog)
	defer os.Remove(fileResult)

	err, tmpDBPath := this.createTmpBD(DataRep.IsExtension())
	if err != nil {
		logrus.WithError(err).Errorf("Произошла ошибка создания временной базы.")
		return ""
	}
	defer os.RemoveAll(tmpDBPath)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	//param = append(param, "/IBName Задание2")
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", DataRep.GetRepPath()))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryN %v", DataRep.GetLogin()))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryP %v", DataRep.GetPass()))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryReport %v", fileResult))
	if versionStart > 0 {
		param = append(param, fmt.Sprintf("-NBegin %d", versionStart))
	}
	if DataRep.IsExtension() {
		param = append(param, "-Extension temp")
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	cmd := exec.Command(this.BinPath, param...)
	if err := this.run(cmd, fileLog); err != nil {
		logrus.Panic(err)
	}

	if err, bytes := ReadFile(fileResult, nil); err == nil {
		return string(*bytes)
	} else {
		logrus.Errorf("Произошла ошибка при чтерии отчета: %v", err)
		return ""
	}
}

func (this *Repository) Destroy() {
	//os.RemoveAll(rep.tmpDBPath)
}

func (this *Repository) run(cmd *exec.Cmd, fileLog string) (err error) {
	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("%v", er)
			logrus.
				WithField("Параметры", cmd.Args).
				Errorf("Произошла ошибка при выполнении %q", cmd.Path)
		}
	}()

	logrus.
		WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		Debug("Выполняется команда пакетного запуска")

	//cmd.Stdin = strings.NewReader("some input")
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	readErrFile := func() string {
		if err, buf := ReadFile(fileLog, charmap.Windows1251.NewDecoder()); err == nil {
			return string(*buf)
		} else {
			logrus.Error(err)
			return ""
		}
	}

	err = cmd.Run()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	if err != nil {
		logrus.Panic(fmt.Errorf("Произошла ошибка запуска:"+
			"err: %q"+
			"OutErrFile: %q", string(err.Error()), readErrFile())) // в defer перехват
	}
	if stderr != "" {
		logrus.Panic(fmt.Errorf("Произошла ошибка запуска:"+
			"StdErr: %q"+
			"OutErrFile: %q", stderr, readErrFile())) // в defer перехват
	}

	return nil
}

//////////////// Common ///////////////////////
func getSubDir(rootDir string) []string {
	var result []string
	f := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			result = append(result, path)
		}

		return nil
	}

	filepath.Walk(rootDir, f)
	return result
}

func FindFiles(rootDir, fileName string) (error, string) {
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		return err, ""
	}

	Files, _ := GetFiles(rootDir)

	for _, file := range Files {
		if _, f := filepath.Split(file); f == fileName {
			return nil, file
		}
	}

	return fmt.Errorf("Файл %q не найден в каталоге %q", fileName, rootDir), ""
}

func GetFiles(DirPath string) ([]string, int64) {
	var result []string
	var size int64
	f := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() || info.Size() == 0 {
			return nil
		} else {
			result = append(result, path)
			size += info.Size()
		}

		return nil
	}

	filepath.Walk(DirPath, f)
	return result, size
}

func ReadFile(filePath string, Decoder *encoding.Decoder) (error, *[]byte) {
	//dec := charmap.Windows1251.NewDecoder()

	if fileB, err := ioutil.ReadFile(filePath); err == nil {
		// Разные кодировки = разные длины символов.
		if Decoder != nil {
			newBuf := make([]byte, len(fileB)*2)
			Decoder.Transform(newBuf, fileB, false)

			return nil, &newBuf
		} else {
			return nil, &fileB
		}
	} else {
		return fmt.Errorf("Ошибка открытия файла %q:\n %v", filePath, err), nil
	}
}
