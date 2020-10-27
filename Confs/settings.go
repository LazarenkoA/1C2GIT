package settings

import (
	"bufio"
	"encoding/json"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strconv"

	xmlpath "gopkg.in/xmlpath.v2"
)

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

type Setting struct {
	Bin1C          string            `json:"Bin1C"`
	RepositoryConf []*RepositoryConf `json:"RepositoryConf"`
	Mongo          *struct {
		ConnectionString string
	} `json:"Mongo"`
	TFS *struct {
		URL string `json:"URL"`
		KEY string `json:"KEY"`
	} `json:"TFS"`
}

func ReadSettings(Filepath string, data interface{}) {
	if _, err := os.Stat(Filepath); os.IsNotExist(err) {
		logrusRotate.StandardLogger().WithField("файл", Filepath).Panic("Конфигурационный файл не найден")
	}

	file, err := ioutil.ReadFile(Filepath)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", Filepath).WithError(err).Panic("Ошибка открытия файла")
	}

	err = json.Unmarshal(file, data)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", Filepath).WithError(err).Panic("Ошибка чтения конфигурационного файла")
	}
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

// legacy
func (this *RepositoryConf) SaveVersion() {
	logrusRotate.StandardLogger().WithField("Репозиторий", this.To.RepDir).WithField("Версия", this.version).Debug("Сохраняем версию расширения")

	ConfigurationFile := path.Join(this.To.RepDir, "Configuration.xml")
	if _, err := os.Stat(ConfigurationFile); os.IsNotExist(err) {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Error("Конфигурационный файл (Configuration.xml) не найден")
		return
	}

	file, err := os.Open(ConfigurationFile)
	if err != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Errorf("Ошибка открытия: %q", err)
		return
	}
	defer file.Close()

	xmlroot, xmlerr := xmlpath.Parse(bufio.NewReader(file))
	if xmlerr != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Errorf("Ошибка чтения xml: %q", xmlerr.Error())
		return
	}

	path := xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Version/text()")
	if value, ok := path.String(xmlroot); ok {
		this.version = value
	} else {
		// значит версии нет, установим начальную
		this.version = "1.0.0"
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Debugf("В файле не было версии, установили %q", this.version)
	}

}

func (this *RepositoryConf) RestoreVersion(version int) {
	logrusRotate.StandardLogger().WithField("Репозиторий", this.To.RepDir).WithField("Версия", this.version).Debug("Восстанавливаем версию расширения")

	ConfigurationFile := path.Join(this.To.RepDir, "Configuration.xml")
	if _, err := os.Stat(ConfigurationFile); os.IsNotExist(err) {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).WithField("Репозиторий", this.GetRepPath()).Error("Конфигурационный файл (Configuration.xml) не найден")
		return
	}

	// Меняем версию, без парсинга, поменять значение одного узла прям проблема, а повторять структуру xml в структуре ой как не хочется
	// Читаем файл
	file, err := os.Open(ConfigurationFile)
	if err != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Errorf("Ошибка открытия файла: %q", err)
		return
	}

	stat, _ := file.Stat()
	buf := make([]byte, stat.Size())
	if _, err = file.Read(buf); err != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Errorf("Ошибка чтения файла: %q", err)
		return
	}
	file.Close()
	os.Remove(ConfigurationFile)

	xml := string(buf)
	reg := regexp.MustCompile(`(?i)(?:<Version>(.+?)<\/Version>|<Version\/>)`)
	//xml = reg.ReplaceAllString(xml, "<Version>"+this.version+"</Version>")
	xml = reg.ReplaceAllString(xml, "<Version>"+strconv.Itoa(version)+"</Version>")

	// сохраняем файл
	file, err = os.OpenFile(ConfigurationFile, os.O_CREATE, os.ModeExclusive)
	if err != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Errorf("Ошибка создания: %q", err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(xml); err != nil {
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Errorf("Ошибка записи: %q", err)
		return
	}
}
