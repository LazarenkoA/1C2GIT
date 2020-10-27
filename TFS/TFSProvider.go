package TFS

import (
	"bytes"
	"encoding/json"
	"fmt"
	settings "github.com/LazarenkoA/1C2GIT/Confs"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type TFSProvider struct {
	url, key string
}

func (t *TFSProvider) New(s *settings.Setting) *TFSProvider {
	if s.TFS == nil {
		return nil
	}

	t.url, t.key = s.TFS.URL, s.TFS.KEY
	return t
}

func (t *TFSProvider) CreateComment(ids []string, text string)  {
	defer func() {
		if err := recover(); err != nil {
			logrusRotate.StandardLogger().WithError(err.(error)).Info("ТФС. Ошибка создания комментария. (recover)")
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
				workItemId :=  item["id"].(float64)

				url := fmt.Sprintf("%s/%s/_apis/wit/workItems/%.0f/comments?api-version=5.1-preview.3", t.url, project, workItemId)
				body, _ := json.Marshal(comment)
				if _, err := t.execRequest(http.MethodPost, url, bytes.NewReader(body)); err != nil {
					logrusRotate.StandardLogger().WithError(err).Info("ТФС. Ошибка создания комментария")
					continue
				}

			}
		}
	}
}

func (t *TFSProvider) appednAuthorizationHead(header http.Header)  {
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
	if body, err := t.execRequest(http.MethodGet, url, nil); err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("body", string(body)).Info("ТФС. Ошибка получения информации по таскам")
	} else {
		if err = json.Unmarshal(body, &result); err != nil {
			logrusRotate.StandardLogger().WithError(err).Info("ТФС. Ошибка десериализации данных")
		}
	}

	return result
}

func (t *TFSProvider) execRequest(method, url string, body io.Reader) ([]byte, error) {
	httpClient := &http.Client{
		Timeout: time.Minute*5,
	}
	var req *http.Request; var err error
	if req, err = http.NewRequest(method, url, body); err != nil {
		return []byte{}, err
	}
	t.appednAuthorizationHead(req.Header)
	req.SetBasicAuth("", t.key)

	if resp, err := httpClient.Do(req); err != nil {
		return []byte{},err
	} else {
		defer resp.Body.Close()
		b, _ := ioutil.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			return b, fmt.Errorf( "StatusCode = %d", resp.StatusCode)
		}

		return b, nil
	}

}