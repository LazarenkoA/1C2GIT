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
	data   I1CCommit
	author string
	env    map[string]string
	//gitBin string // если стоит git, то в системной переменной path будет путь к git
}

type I1CCommit interface {
	GetComment() string
	GetAuthor() string
	GetDateTime() *time.Time
}

// New - конструктор
func (g *Git) New(repDir string, data I1CCommit, mapUser map[string]string) *Git {
	g.repDir = repDir
	g.data = data

	if g.author = mapUser[g.data.GetAuthor()]; g.author == "" {
		if g.author = mapUser["Default"]; g.author == "" {
			logrus.Panic("В конфиге MapUsers.conf не определен Default пользователь")
		}
	}

	g.env = make(map[string]string) // что бы в Destroy вернуть то что было
	g.env["GIT_AUTHOR_NAME"] = os.Getenv("GIT_AUTHOR_NAME")
	g.env["GIT_COMMITTER_NAME"] = os.Getenv("GIT_COMMITTER_NAME")
	g.env["GIT_AUTHOR_EMAIL"] = os.Getenv("GIT_AUTHOR_EMAIL")
	g.env["GIT_COMMITTER_EMAIL"] = os.Getenv("GIT_COMMITTER_EMAIL")

	parts := strings.SplitN(g.author, " ", 2)
	// говорят, что лучше указывать переменные окружения для коммитов
	os.Setenv("GIT_AUTHOR_NAME", strings.Trim(parts[0], " "))
	os.Setenv("GIT_COMMITTER_NAME", strings.Trim(parts[0], " "))
	os.Setenv("GIT_AUTHOR_EMAIL", strings.Trim(parts[1], " "))
	os.Setenv("GIT_COMMITTER_EMAIL", strings.Trim(parts[1], " "))

	return g
}

func (g *Git) Destroy() {
	for k, v := range g.env {
		os.Setenv(k, v)
	}
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

func (g *Git) Add() (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("Add")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "add", ".")
	if err, _ := g.run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) optimization() (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("Add")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "gc", "--auto")
	if err, _ := g.run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) CommitAndPush(branch string) (err error) {
	logrus.WithField("Каталог", g.repDir).Debug("CommitAndPush")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	g.checkout(branch)
	g.Add()

	date := g.data.GetDateTime().Format("2006.01.02 15:04:05")

	param := []string{}
	param = append(param, "commit")
	//param = append(param, "-a")
	param = append(param, "--allow-empty-message")
	param = append(param, fmt.Sprintf("--cleanup=verbatim"))
	param = append(param, fmt.Sprintf("--date=%v", date))
	if g.author != "" {
		param = append(param, fmt.Sprintf("--author=%q", g.author))
	}
	param = append(param, fmt.Sprintf("-m %v", g.data.GetComment()))
	param = append(param, strings.Replace(g.repDir, "\\", "/", -1)) // strings.Replace(g.repDir, "\\", "/", -1)

	cmdCommit := exec.Command("git", param...)
	g.run(cmdCommit, g.repDir)

	g.Pull(branch)
	g.Push()
	g.optimization()

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
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%q \n", string(err.Error()))
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%q \n", stderr)
		}
		logrus.Error(errText)
		return fmt.Errorf(errText), ""
	}

	return nil, cmd.Stdout.(*bytes.Buffer).String()
}
