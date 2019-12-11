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
	//mu     *sync.Mutex
	//gitBin string // если стоит git, то в системной переменной path будет путь к git
}

type I1CCommit interface {
	GetComment() string
	GetAuthor() string
	GetDateTime() *time.Time
}

// New - конструктор
func (g *Git) New(repDir string, data I1CCommit, mapUser map[string]string) *Git { // mu *sync.Mutex,
	g.repDir = repDir
	g.data = data
	//g.mu = mu

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
	logrus.WithField("Каталог", g.repDir).Debug("GIT. Destroy")

	for k, v := range g.env {
		os.Setenv(k, v)
	}
}

func (g *Git) Сheckout(branch, repDir string, notLock bool) error {
	logrus.WithField("Каталог", g.repDir).Debug("GIT. Сheckout")

	// notLock нужен для того, что бы не заблокировать самого себя, например вызов такой
	// CommitAndPush - Pull - Сheckout, в этом случаи не нужно лочить т.к. в CommitAndPush уже залочено
	// if !notLock {
	// 	g.mu.Lock()
	// 	defer g.mu.Unlock()
	// }

	if cb, err := g.getCurrentBranch(); err != nil {
		return err
	} else if cb == branch {
		// Если текущая ветка = ветки назначения, просто выходим
		return nil
	}

	logrus.WithField("Каталог", repDir).WithField("branch", branch).Debug("checkout")

	cmd := exec.Command("git", "checkout", branch)
	if _, err := run(cmd, repDir); err != nil {
		return err // Странно, но почему-то гит информацию о том что изменилась ветка пишет в Stderr
	} else {
		return nil
	}
}

func (g *Git) getCurrentBranch() (result string, err error) {
	var branches []string
	if branches, err = g.getBranches(); err != nil {
		return "", err
	}
	for _, b := range branches {
		// только так получилось текущую ветку определить
		if strings.Index(b, "*") > -1 {
			return strings.Trim(b, " *"), nil
		}
	}

	return "", fmt.Errorf("Не удалось определить текущую ветку.\nДоступные ветки %v", branches)
}

func (g *Git) getBranches() (result []string, err error) {
	logrus.WithField("Каталог", g.repDir).Debug("GIT. getBranches")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}
	result = []string{}

	cmd := exec.Command("git", "branch")
	if res, err := run(cmd, g.repDir); err != nil {
		return []string{}, err
	} else {
		for _, branch := range strings.Split(res, "\n") {
			if branch == "" {
				continue
			}
			result = append(result, strings.Trim(branch, " "))
		}
	}
	return result, nil
}

func (g *Git) Pull(branch string) (err error) {
	// g.mu.Lock()
	// defer g.mu.Unlock()

	logrus.WithField("Каталог", g.repDir).Debug("GIT. Pull")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	if branch != "" {
		g.Сheckout(branch, g.repDir, true)
	}

	cmd := exec.Command("git", "pull")
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) Push() (err error) {
	// g.mu.Lock()
	// defer g.mu.Unlock()
	logrus.WithField("Каталог", g.repDir).Debug("GIT. Push")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "push")
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) Add() (err error) {
	// g.mu.Lock()
	// defer g.mu.Unlock()

	logrus.WithField("Каталог", g.repDir).Debug("GIT. Add")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "add", ".")
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) ResetHard(branch string) (err error) {
	// g.mu.Lock()
	// defer g.mu.Unlock()

	logrus.WithField("Каталог", g.repDir).Debug("GIT. ResetHard")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	if branch != "" {
		g.Сheckout(branch, g.repDir, true)
	}

	cmd := exec.Command("git", "reset", "--hard", "origin/"+branch)
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) optimization() (err error) {

	logrus.WithField("Каталог", g.repDir).Debug("GIT. optimization")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	cmd := exec.Command("git", "gc", "--auto")
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) CommitAndPush(branch string) (err error) {
	// закоментировал, что бы не было дедлока т.е. в методах типа Add, Pull ... тоже лок накладывается
	// g.mu.Lock()
	// defer g.mu.Unlock()

	logrus.WithField("Каталог", g.repDir).Debug("GIT. CommitAndPush")

	defer func() { logrus.WithField("Каталог", g.repDir).Debug("end CommitAndPush") }()

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("Каталог %q Git репозитория не найден", g.repDir)
		logrus.WithField("Каталог", g.repDir).Error(err)
	}

	g.Add()
	g.Pull(branch)

	//  весь метот лочить не можем
	// g.mu.Lock()
	// func() {
	// 	defer g.mu.Unlock()

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
	param = append(param, strings.Replace(g.repDir, "\\", "/", -1))

	cmdCommit := exec.Command("git", param...)
	run(cmdCommit, g.repDir)
	// }()

	g.Push()
	g.optimization()

	return nil
}

func run(cmd *exec.Cmd, dir string) (string, error) {
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
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%v \n Параметры: %v", string(err.Error()), cmd.Args)
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%v \n", stderr)
		}
		logrus.WithField("Исполняемый файл", cmd.Path).Error(errText)
		return "", fmt.Errorf(errText)
	}
	return cmd.Stdout.(*bytes.Buffer).String(), err
}
