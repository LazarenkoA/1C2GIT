package ConfigurationRepository

import (
	"bytes"
	"errors"
	"fmt"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"golang.org/x/text/encoding"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Repository struct {
	binPath string
	logger  *logrus.Entry
	mu      *sync.Mutex
}

type IRepositoryConf interface {
	GetRepPath() string
	GetLogin() string
	GetPass() string
	IsExtension() bool
	GetTimerDuration() time.Duration
	GetDir() string
	GetBranch() string
}

type Notify struct {
	RepInfo IRepositoryConf
	Comment string
	Version int
	Author  string
	Date    time.Time
	Err     error
}

const (
	temCfeName      = "temp"
	versionFileName = "versions"
)

func (this *Repository) New(binPath string) *Repository {
	this.binPath = binPath
	this.logger = logrusRotate.StandardLogger().WithField("name", "Repository")
	this.mu = new(sync.Mutex)

	return this
}

func (r *Notify) GetComment() string {
	return r.Comment
}

func (r *Notify) GetAuthor() string {
	return strings.Trim(r.Author, " ")
}

func (r *Notify) GetDateTime() *time.Time {
	return &r.Date
}

func (this *Repository) createTmpFile() string {
	//currentDir, _ := os.Getwd()
	fileLog, err := ioutil.TempFile("", "OutLog_")
	if err != nil {
		panic(fmt.Errorf("Ошибка получения временного файла:\n %v", err))
	}

	fileLog.Close() // Закрываем иначе в него 1С не сможет записать
	return fileLog.Name()
}

// CreateTmpBD метод создает временную базу данных
func (this *Repository) createTmpBD(tmpDBPath string, withExtension bool) (err error) {
	var Ext string

	if withExtension {
		currentDir, _ := os.Getwd()
		Ext = filepath.Join(currentDir, "tmp.cfe")

		if _, err := os.Stat(Ext); os.IsNotExist(err) {
			return fmt.Errorf("В каталоге с программой не найден файл расширения tmp.cfe")
		}
	}

	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("произошла ошибка при создании временной базы: %v", er)
			this.logger.Error(err)
			os.RemoveAll(tmpDBPath)
		}
	}()

	fileLog := this.createTmpFile()
	defer func() {
		os.Remove(fileLog)
	}()

	var param []string

	if withExtension {
		param = append(param, "DESIGNER")
		param = append(param, "/F", tmpDBPath)
		param = append(param, "/DisableStartupDialogs")
		param = append(param, "/DisableStartupMessages")
		param = append(param, "/LoadCfg", Ext)
		param = append(param, "-Extension", temCfeName)
	} else {
		param = append(param, "CREATEINFOBASE")
		param = append(param, fmt.Sprintf("File='%s'", tmpDBPath))
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	cmd := exec.Command(this.binPath, param...)
	if err := this.run(cmd, fileLog); err != nil {
		this.logger.WithError(err).Panic("Ошибка создания информационной базы.")
	}

	this.logger.Debug(fmt.Sprintf("Создана tempDB '%s'", tmpDBPath))

	return nil
}

func (this *Repository) getReport(DataRep IRepositoryConf, version int) ([]*Notify, error) {
	var result []*Notify

	report := this.saveReport(DataRep, version)
	if report == "" {
		return result, fmt.Errorf("получен пустой отчет по хранилищу %v", DataRep.GetRepPath())
	}

	// Двойные кавычки в комментарии мешают, по этому мы заменяем из на одинарные
	report = strings.Replace(report, "\"\"", "'", -1)

	var tmpArray [][]string
	reg := regexp.MustCompile(`[{]"#","([^"]+)["][}]`)
	matches := reg.FindAllStringSubmatch(report, -1)
	for _, s := range matches {
		if s[1] == "Версия:" {
			tmpArray = append(tmpArray, []string{})
		}

		if len(tmpArray) > 0 {
			tmpArray[len(tmpArray)-1] = append(tmpArray[len(tmpArray)-1], s[1])
		}
	}

	r := strings.NewReplacer("\r", "", "\n", " ")
	for _, array := range tmpArray {
		RepInfo := &Notify{RepInfo: DataRep}
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
					RepInfo.Comment = r.Replace(array[id+1])
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

	return result, nil
}

func (this *Repository) saveReport(DataRep IRepositoryConf, versionStart int) string {
	defer func() {
		if er := recover(); er != nil {
			this.logger.Error(fmt.Errorf("произошла ошибка при получении истории из хранилища: %v", er))
		}
	}()

	this.logger.Debug("Сохраняем отчет конфигурации в файл")

	//currentDir, _ := os.Getwd()
	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")
	defer os.RemoveAll(tmpDBPath)

	if err := this.createTmpBD(tmpDBPath, DataRep.IsExtension()); err != nil {
		this.logger.WithError(err).Errorf("Произошла ошибка создания временной базы.")
		return ""
	}

	fileLog := this.createTmpFile()
	fileResult := this.createTmpFile()
	defer func() {
		os.Remove(fileLog)
		os.Remove(fileResult)
	}()

	var param []string
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, "/F", tmpDBPath)

	param = append(param, "/ConfigurationRepositoryF", DataRep.GetRepPath())
	param = append(param, "/ConfigurationRepositoryN", DataRep.GetLogin())
	param = append(param, "/ConfigurationRepositoryP", DataRep.GetPass())
	param = append(param, "/ConfigurationRepositoryReport", fileResult)
	if versionStart > 0 {
		param = append(param, fmt.Sprintf("-NBegin %d", versionStart))
	}
	if DataRep.IsExtension() {
		param = append(param, "-Extension", temCfeName)
	}
	param = append(param, "/OUT", fileLog)

	cmd := exec.Command(this.binPath, param...)

	if err := this.run(cmd, fileLog); err != nil {
		this.logger.Panic(err)
	}

	if b, err := this.readFile(fileResult, nil); err == nil {
		return string(b)
	} else {
		this.logger.Errorf("Произошла ошибка при чтерии отчета: %v", err)
		fmt.Printf("Произошла ошибка при чтерии отчета: %v", err)
		return ""
	}
}

func (this *Repository) run(cmd *exec.Cmd, fileLog string) (err error) {
	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("%v", er)
			this.logger.WithField("Параметры", cmd.Args).Errorf("Произошла ошибка при выполнении %q", cmd.Path)
		}
	}()

	this.logger.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		Debug("Выполняется команда пакетного запуска")

	timeout := time.Hour
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	errch := make(chan error, 1)

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("Произошла ошибка запуска:\n\terr:%v\n\tПараметры: %v\n\t", err.Error(), cmd.Args)
	}

	// запускаем в горутине т.к. наблюдалось что при выполнении команд в пакетном режиме может происходить зависон, нам нужен таймаут
	go func() {
		errch <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout): // timeout
		// завершаем процесс
		cmd.Process.Kill()
		return fmt.Errorf("Выполнение команды прервано по таймауту\n\tПараметры: %v\n\t", cmd.Args)
	case err := <-errch:
		if err != nil {
			stderr := cmd.Stderr.(*bytes.Buffer).String()
			errText := fmt.Sprintf("Произошла ошибка запуска:\n\terr:%v\n\tПараметры: %v\n\t", err.Error(), cmd.Args)
			if stderr != "" {
				errText += fmt.Sprintf("StdErr:%v\n", stderr)
			}

			if buf, err := this.readFile(fileLog, nil); err == nil {
				errText += string(buf)
			}

			return errors.New(errText)
		} else {
			return nil
		}
	}
}

func (this *Repository) getLastVersion(DataRep IRepositoryConf) (version int, err error) {
	this.logger.Debug(fmt.Sprintf("Читаем последнюю синхронизированную версию для %v\n", DataRep.GetRepPath()))

	this.mu.Lock()
	defer this.mu.Unlock()

	vInfo, err := this.readVersionsFile()
	if err != nil {
		this.logger.Error(fmt.Sprintf("Ошибка при чтении файла версий: %v\n", err))
		return 0, err
	}

	return vInfo[DataRep.GetRepPath()], nil
}

func (this *Repository) saveLastVersion(DataRep IRepositoryConf, newVersion int) (err error) {
	this.logger.Debug(fmt.Sprintf("Записываем последнюю синхронизированную версию для %v (%v)\n", DataRep.GetRepPath(), newVersion))

	this.mu.Lock()
	defer this.mu.Unlock()

	// при записи в общий файл может получится потеря данных, когда данные последовательно считываются, потом в своем потоке меняется своя версия расширения
	// при записи в файл версия другого расширения затирается
	// по этому, перед тем как записать, еще раз считываем с диска
	vInfo, err := this.readVersionsFile()
	if err != nil {
		this.logger.Error(fmt.Sprintf("Ошибка при чтении файла версий: %v\n", err))
		return err
	}

	vInfo[DataRep.GetRepPath()] = newVersion

	currentDir, _ := os.Getwd()
	filePath := filepath.Join(currentDir, versionFileName)

	b, err := yaml.Marshal(vInfo)
	if err != nil {
		err = fmt.Errorf("ошибка сериализации: %v", err)
		this.logger.Error(fmt.Sprintf("Ошибка при записи файла версий: %v\n", err))
		return
	}

	if err = ioutil.WriteFile(filePath, b, os.ModeAppend|os.ModePerm); err != nil {
		err = fmt.Errorf("ошибка записи файла %q", filePath)
		this.logger.Error(fmt.Sprintf("Ошибка при записи файла версий: %v\n", err))
		return
	}

	return err
}

func (this *Repository) Observe(repInfo IRepositoryConf, wg *sync.WaitGroup, notify func(*Notify, *Repository) error) {
	defer wg.Done()

	l := this.logger.WithField("Репозиторий", repInfo.GetRepPath())
	if repInfo.GetTimerDuration().Minutes() <= 0 {
		l.Error("для репазитория не определен параметр TimerMinute")
		return
	}

	timer := time.NewTicker(repInfo.GetTimerDuration())
	defer timer.Stop()

	for {
		func() {
			version, err := this.getLastVersion(repInfo)
			if err != nil {
				l.WithError(err).Error("ошибка получения последней синхронизированной версиии")
				return
			}

			l.WithField("Начальная ревизия", version).Debug("Старт выгрузки")
			report, err := this.getReport(repInfo, version+1)
			if err != nil {
				l.WithError(err).Error("ошибка получения отчета по хранилищу")
				return
			}
			if len(report) == 0 {
				l.Debug("новых версий не найдено")
				return
			}

			for _, _report := range report {
				if err := notify(_report, this); err == nil {
					if e := this.saveLastVersion(repInfo, _report.Version); e != nil {
						l.WithError(e).Error("ошибка обновления последней синхронизированной версиии")
					}
				}
			}
		}()

		<-timer.C
	}
}

func (this *Repository) readFile(filePath string, Decoder *encoding.Decoder) ([]byte, error) {
	if fileB, err := ioutil.ReadFile(filePath); err == nil {
		// Разные кодировки = разные длины символов.
		if Decoder != nil {
			newBuf := make([]byte, len(fileB)*2)
			Decoder.Transform(newBuf, fileB, false)

			return newBuf, nil
		} else {
			return fileB, nil
		}
	} else {
		return []byte{}, fmt.Errorf("Ошибка открытия файла %q:\n %v", filePath, err)
	}
}

func (this *Repository) RestoreVersion(n *Notify) {
	l := this.logger.WithField("Репозиторий", n.RepInfo.GetDir()).WithField("Версия", n.Version)
	l.Debug("Восстанавливаем версию расширения")

	ConfigurationFile := path.Join(n.RepInfo.GetDir(), "Configuration.xml")
	if _, err := os.Stat(ConfigurationFile); os.IsNotExist(err) {
		l.WithField("Файл", ConfigurationFile).Error("конфигурационный файл не найден")
		return
	}

	// Меняем версию, без парсинга, поменять значение одного узла прям проблема, а повторять структуру xml в структуре ой как не хочется
	// Читаем файл
	file, err := os.Open(ConfigurationFile)
	if err != nil {
		l.WithField("Файл", ConfigurationFile).Errorf("Ошибка открытия файла: %q", err)
		return
	}

	stat, _ := file.Stat()
	buf := make([]byte, stat.Size())
	if _, err = file.Read(buf); err != nil {
		l.WithField("Файл", ConfigurationFile).Errorf("Ошибка чтения файла: %q", err)
		return
	}
	file.Close()
	os.Remove(ConfigurationFile)

	xml := string(buf)
	reg := regexp.MustCompile(`(?i)(?:<Version>(.+?)</Version>|<Version/>)`)
	//xml = reg.ReplaceAllString(xml, "<Version>"+this.version+"</Version>")
	xml = reg.ReplaceAllString(xml, "<Version>"+strconv.Itoa(n.Version)+"</Version>")

	// сохраняем файл
	file, err = os.OpenFile(ConfigurationFile, os.O_CREATE, os.ModeExclusive)
	if err != nil {
		l.WithError(err).WithField("Файл", ConfigurationFile).Error("Ошибка создания")
		return
	}
	defer file.Close()

	if _, err := file.WriteString(xml); err != nil {
		l.WithError(err).WithField("Файл", ConfigurationFile).Error("Ошибка записи")
		return
	}
}

func (this *Repository) readVersionsFile() (vInfo map[string]int, err error) {

	currentDir, _ := os.Getwd()
	filePath := filepath.Join(currentDir, versionFileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл версий не найден %q", filePath)
	}

	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия файла версий %q", filePath)
	}

	vInfo = make(map[string]int, 0)
	err = yaml.Unmarshal(file, &vInfo)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла весрий %q", filePath)
	}

	return vInfo, nil
}

// Выгрузка конфигурации в файлы
func (this *Repository) DownloadConfFiles(repInfo IRepositoryConf, version int) (err error) {
	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("произошла ошибка при сохранении конфигурации конфигурации в файлы: %v", er)
		}
	}()

	this.logger.Debug("Сохраняем конфигурацию в файлы")

	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")
	defer os.RemoveAll(tmpDBPath)

	if err = this.createTmpBD(tmpDBPath, repInfo.IsExtension()); err != nil {
		return err
	}

	// ПОДКЛЮЧАЕМ к ХРАНИЛИЩУ и ОБНОВЛЯЕМ ДО ОПРЕДЕЛЕННОЙ ВЕРСИИ
	this.configurationRepositoryBindCfg(repInfo, tmpDBPath, version)

	// СОХРАНЯЕМ В ФАЙЛЫ
	this.dumpConfigToFiles(repInfo, tmpDBPath)

	return nil
}

func (this *Repository) configurationRepositoryBindCfg(DataRep IRepositoryConf, fileDBPath string, version int) {
	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)

	var param []string
	param = append(param, "DESIGNER")
	param = append(param, "/F", fileDBPath)
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, "/ConfigurationRepositoryF", DataRep.GetRepPath())
	param = append(param, "/ConfigurationRepositoryN", DataRep.GetLogin())
	param = append(param, "/ConfigurationRepositoryP", DataRep.GetPass())
	param = append(param, "/ConfigurationRepositoryBindCfg")
	param = append(param, "-forceBindAlreadyBindedUser")
	param = append(param, "-forceReplaceCfg")
	if DataRep.IsExtension() {
		param = append(param, "-Extension", temCfeName)
	}

	param = append(param, "/ConfigurationRepositoryUpdateCfg")
	param = append(param, fmt.Sprintf("-v %d", version))
	param = append(param, "-force")
	param = append(param, "-revised")
	if DataRep.IsExtension() {
		param = append(param, "-Extension", temCfeName)
	}

	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	if err := this.run(exec.Command(this.binPath, param...), fileLog); err != nil {
		this.logger.Panic(err)
	}
}

func (this *Repository) dumpConfigToFiles(DataRep IRepositoryConf, fileDBPath string) {
	fileLog := this.createTmpFile()
	defer os.Remove(fileLog)

	var param []string
	param = append(param, "DESIGNER")
	param = append(param, "/F", fileDBPath)
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/DumpConfigToFiles %v", DataRep.GetDir()))
	if DataRep.IsExtension() {
		param = append(param, "-Extension", temCfeName)
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	if err := this.run(exec.Command(this.binPath, param...), fileLog); err != nil {
		this.logger.Panic(err)
	}
}
