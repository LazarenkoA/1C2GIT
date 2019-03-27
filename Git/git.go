package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type Git struct {
	repDir string
	//gitBin string // если стоит git, то в системной переменной path будет путь к git
}

type ICommit interface {
	GetComment() string
	GetAuthor() string
	GetDateTime() *time.Time
}

// New - конструктор
func (g *Git) New(repDir string) *Git {
	g.repDir = repDir
	return g
}

func (g *Git) checkout(branch string) error {
	logrus.WithField("Каталог", g.repDir).Debug("checkout")

	cmd := exec.Command("git", "checkout", branch)
	if err, _ := g.run(cmd, g.repDir); err != nil {
		return err // Странно, но почему-то гит информацию о том что изменилась ветка пишет в Stderr
	} else {
		return nil
	}
}

func (g *Git) Pull(branch string) (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("Pull")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	if branch != "" {
		g.checkout(branch)
	}

	cmd := exec.Command("git", "pull")
	if err, _ := g.run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) GetBranches() (err error, result []string) {
	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	result = []string{}

	cmd := exec.Command("git", "branch")
	if err, res := g.run(cmd, g.repDir); err != nil {
		return err, []string{}
	} else {
		for _, branch := range strings.Split(res, "\n") {
			if branch == "" {
				continue
			}
			result = append(result, strings.Trim(branch, " *"))
		}
		return nil, result
	}
}

func (g *Git) Push() (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("Push")
	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "push")
	if err, _ := g.run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) CommitAndPush(data ICommit, mapUser map[string]string, branch string) (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("CommitAndPush")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	param := []string{}
	param = append(param, "commit")
	param = append(param, "-a")
	param = append(param, "--amend")
	param = append(param, fmt.Sprintf("--date=%v", data.GetDateTime().Format("2006.01.02 15:04:05")))

	coment := data.GetComment()
	if coment == "" {
		coment = fmt.Sprintf("Коммит без комментария")
	}

	author := mapUser[data.GetAuthor()]
	if author == "" {
		author = mapUser["Default"]
	}
	if author != "" {
		param = append(param, fmt.Sprintf("--author=%q", author))
	}
	param = append(param, fmt.Sprintf("-m %v", coment))
	cmdCommit := exec.Command("git", param...)
	g.run(cmdCommit, g.repDir)

	g.Pull(branch)
	g.Push()

	return nil
}

func (g *Git) run(cmd *exec.Cmd, dir string) (error, string) {
	logrus.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		WithField("Каталог", dir).
		Debug("Выполняется команда git")

	cmd.Dir = dir
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	err := cmd.Run()
	stderr := string(cmd.Stderr.(*bytes.Buffer).Bytes())
	if stderr != "" {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nStdErr:%q", stderr)
		//logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nerr:%q", string(err.Error()))
		//logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}

	return nil, string(cmd.Stdout.(*bytes.Buffer).Bytes())
}
