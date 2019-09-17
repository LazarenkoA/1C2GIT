package settings

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
)

func ReadSettings(Filepath string, data interface{}) {
	if _, err := os.Stat(Filepath); os.IsNotExist(err) {
		logrus.WithField("файл", Filepath).Panic("Конфигурационный файл не найден")
		return
	}

	file, err := ioutil.ReadFile(Filepath)
	if err != nil {
		logrus.WithField("файл", Filepath).WithError(err).Panic("Ошибка открытия файла")
		return
	}

	err = json.Unmarshal(file, data)
	if err != nil {
		logrus.WithField("файл", Filepath).WithError(err).Panic("Ошибка чтения конфигурационного файла")
		return
	}
}
