package TFS

import (
	"bytes"
	"encoding/json"
	"fmt"
	settings "github.com/LazarenkoA/1C2GIT/Confs"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type TFSProvider struct {
	url, key string
	logger   *logrus.Entry
}

func (t *TFSProvider) New(s *settings.Setting) *TFSProvider {
	if s.TFS == nil {
		return nil
	}

	t.url, t.key = s.TFS.URL, s.TFS.KEY
	t.logger = logrusRotate.StandardLogger().WithField("name", "TFS")
	return t
}

func (t *TFSProvider) CreateComment(ids []string, text string) {
	defer func() {
		if err := recover(); err != nil {
			if e, ok := err.(error); ok {
				t.logger.WithError(e).Info("Ошибка создания комментария. (recover)")
			}
		}
	}()

	wiInfo := t.workItemInfo(ids)
	comment := map[string]string{
		"text": text,
	}

	if value, ok := wiInfo["value"]; ok {
		for _, v := range value.([]interface{}) {
			if item, ok := v.(map[string]interface{}); ok {
				project := item["fields"].(map[string]interface{})["System.TeamProject"].(string)
				workItemId := item["id"].(float64)

				url := fmt.Sprintf("%s/%s/_apis/wit/workItems/%.0f/comments?api-version=5.1-preview.3", t.url, project, workItemId)
				l := t.logger.WithField("url", url)

				body, _ := json.Marshal(comment)
				if b, err := t.execRequest(http.MethodPost, url, bytes.NewReader(body)); err != nil {
					l.WithError(err).WithField("body", string(b)).Error("Ошибка создания комментария")
					continue
				} else {
					l.Info("комментарий усмешно создан")
				}

			}
		}
	}
}

func (t *TFSProvider) appednAuthorizationHead(header http.Header) {
	header.Add("Access-Control-Allow-Credentials", "true")
	header.Add("Access-Control-Allow-Origin", "")
	header.Add("Access-Control-Allow-Headers", "X-Requested-With, Content-Type, Accept, Origin, Authorization")
	header.Add("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	header.Add("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	header.Add("Cache-Control", "post-check=0, pre-check=0")
	header.Add("Pragma", "no-cache")
	header.Add("Content-Type", "application/json")
	header.Add("Accepts", "application/json")
}

func (t *TFSProvider) workItemInfo(ids []string) (result map[string]interface{}) {
	url := fmt.Sprintf("%s/_apis/wit/workItems?ids=%s", t.url, strings.Join(ids, ","))
	l := t.logger.WithField("url", url)

	if body, err := t.execRequest(http.MethodGet, url, nil); err != nil {
		l.WithError(err).WithField("body", string(body)).Error("Ошибка получения информации по таскам")
	} else {
		if err = json.Unmarshal(body, &result); err != nil {
			l.WithError(err).Info("Ошибка десериализации данных")
		}
	}

	return result
}

func (t *TFSProvider) execRequest(method, url string, body io.Reader) ([]byte, error) {
	httpClient := &http.Client{
		Timeout: time.Minute * 5,
	}
	var req *http.Request
	var err error
	if req, err = http.NewRequest(method, url, body); err != nil {
		return []byte{}, err
	}
	t.appednAuthorizationHead(req.Header)
	req.SetBasicAuth("", t.key)

	if resp, err := httpClient.Do(req); err != nil {
		return []byte{}, err
	} else {
		defer resp.Body.Close()
		b, _ := ioutil.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			return b, fmt.Errorf("StatusCode = %d", resp.StatusCode)
		}

		return b, nil
	}

}
