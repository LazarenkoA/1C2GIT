package git

import (
	"bytes"
	"fmt"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Git struct {
	repDir string
	data   I1CCommit
	author string
	env    map[string]string
	logger *logrus.Entry

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
	g.logger = logrusRotate.StandardLogger().WithField("name", "GIT")
	g.logger.WithField("Каталог", g.repDir).Debug("Create object")
	//g.mu = mu

	g.logger.WithField("Пользователь из хранилища", g.data.GetAuthor()).
		WithField("mapUser", mapUser).
		Debug("Получаем соответствие пользователей")
	if g.author = mapUser[g.data.GetAuthor()]; g.author == "" {
		if g.author = mapUser["Default"]; g.author == "" {
			g.logger.Panic("В конфиге MapUsers.conf не определен Default пользователь")
		}
	}
	g.logger.WithField("Автор", g.author).Debug("Create object")

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

	g.logger.WithField("Environ", os.Environ()).Debug("Create object")

	return g
}

func (g *Git) Destroy() {
	g.logger.WithField("Каталог", g.repDir).Debug("Destroy")

	for k, v := range g.env {
		os.Setenv(k, v)
	}

	g.logger.WithField("Environ", os.Environ()).Debug("Восстанавливаем переменные окружения")
}

func (g *Git) Checkout(branch, repDir string) error {
	g.logger.WithField("Каталог", g.repDir).Debug("Checkout")

	// notLock нужен для того, что бы не заблокировать самого себя, например вызов такой
	// CommitAndPush - Pull - Checkout, в этом случаи не нужно лочить т.к. в CommitAndPush уже залочено
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

	g.logger.WithField("Каталог", repDir).WithField("branch", branch).Debug("checkout")

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
	g.logger.WithField("Каталог", g.repDir).Debug(" getBranches")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
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

	g.logger.WithField("Каталог", g.repDir).Debug("Pull")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
	}

	if branch != "" {
		g.Checkout(branch, g.repDir)
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
	g.logger.WithField("Каталог", g.repDir).Debug("Push")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
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

	g.logger.WithField("Каталог", g.repDir).Debug("Add")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
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

	g.logger.WithField("branch", branch).WithField("Каталог", g.repDir).Debug("ResetHard")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
	}

	if branch != "" {
		g.Checkout(branch, g.repDir)
	}
	g.logger.WithField("branch", branch).WithField("Каталог", g.repDir).Debug("fetch")

	cmd := exec.Command("git", "fetch", "origin")
	run(cmd, g.repDir)

	cmd = exec.Command("git", "reset", "--hard", "origin/"+branch)
	if _, err := run(cmd, g.repDir); err != nil {
		return err
	} else {
		return nil
	}
}

func (g *Git) optimization() (err error) {

	g.logger.WithField("Каталог", g.repDir).Debug("optimization")

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
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

	g.logger.WithField("Каталог", g.repDir).Debug("CommitAndPush")

	defer func() { g.logger.WithField("Каталог", g.repDir).Debug("end CommitAndPush") }()

	if _, err = os.Stat(g.repDir); os.IsNotExist(err) {
		err = fmt.Errorf("каталог %q Git репозитория не найден", g.repDir)
		g.logger.WithField("Каталог", g.repDir).Error(err)
	}

	err = g.Add()
	err = g.Pull(branch)
	if err != nil {
		return
	}

	//  весь метот лочить не можем
	// g.mu.Lock()
	// func() {
	// 	defer g.mu.Unlock()

	date := g.data.GetDateTime().Format("2006.01.02 15:04:05")

	var param []string
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
	if _, err = run(cmdCommit, g.repDir); err != nil {
		return err
	}
	// }()

	err = g.Push()
	err = g.optimization()

	return nil
}

func run(cmd *exec.Cmd, dir string) (string, error) {
	logrusRotate.StandardLogger().WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		WithField("Каталог", dir).
		Debug("Выполняется команда git")

	cmd.Dir = dir
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	err := cmd.Run()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	stdout := cmd.Stdout.(*bytes.Buffer).String()

	// Гит странный, вроде информационное сообщение как "nothing to commit, working tree clean" присылает в Stderr и статус выполнения 1, ну еба...
	// приходится костылить
	if err != nil && !strings.Contains(stdout, "nothing to commit") {
		errText := fmt.Sprintf("произошла ошибка запуска:\n err:%v \n Параметры: %v", err.Error(), cmd.Args)
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%v \n", stderr)
		}
		logrusRotate.StandardLogger().WithField("Исполняемый файл", cmd.Path).
			WithField("Stdout", stdout).
			Error(errText)
		return stdout, fmt.Errorf(errText)
	} else {
		return stdout, nil
	}
}
