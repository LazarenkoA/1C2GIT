package settings

import (
	"bufio"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path"
	"time"

	"gopkg.in/xmlpath.v2"
)

type Destination struct {
	RepDir string `yaml:"RepDir"`
	Branch string `yaml:"Branch"`
}

type RepositoryConf struct {
	TimerMinute int `yaml:"TimerMinute"`
	From        *struct {
		Rep       string `yaml:"Rep"`
		Login     string `yaml:"Login"`
		Pass      string `yaml:"Pass"`
		Extension bool   `yaml:"Extension"`
	} `yaml:"From"`
	To      *Destination `yaml:"To"`
	version string       // для хранения версии конфигурации
}

type Setting struct {
	Bin1C          string            `yaml:"Bin1C"`
	RepositoryConf []*RepositoryConf `yaml:"RepositoryConf"`
	Mongo          *struct {
		ConnectionString string `yaml:"ConnectionString"`
	} `yaml:"Mongo"`
	TFS *struct {
		URL string `yaml:"URL"`
		KEY string `yaml:"KEY"`
	} `yaml:"TFS"`
}

func ReadSettings(Filepath string, data interface{}) {
	if _, err := os.Stat(Filepath); os.IsNotExist(err) {
		logrusRotate.StandardLogger().WithField("файл", Filepath).Panic("Конфигурационный файл не найден")
	}

	file, err := ioutil.ReadFile(Filepath)
	if err != nil {
		logrusRotate.StandardLogger().WithField("файл", Filepath).WithError(err).Panic("Ошибка открытия файла")
	}

	err = yaml.Unmarshal(file, data)
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

func (r *RepositoryConf) GetDestination() *Destination {
	return r.To
}

func (r *RepositoryConf) GetTimerDuration() time.Duration {
	return time.Minute * time.Duration(r.TimerMinute)
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

	path_ := xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Version/text()")
	if value, ok := path_.String(xmlroot); ok {
		this.version = value
	} else {
		// значит версии нет, установим начальную
		this.version = "1.0.0"
		logrusRotate.StandardLogger().WithField("Файл", ConfigurationFile).Debugf("В файле не было версии, установили %q", this.version)
	}

}

func (r *RepositoryConf) GetDir() string {
	return r.To.RepDir
}
func (r *RepositoryConf) GetBranch() string {
	return r.To.Branch
}
