package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"slop-generator/internal/config"
	"slop-generator/internal/gitrepo"
	"slop-generator/internal/history"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
	"slop-generator/internal/task"
	"slop-generator/internal/tui"
)

func main() {
	configPath := flag.String("config", "config.yaml", "YAML configuration path")
	data := flag.String("data-dir", "", "task history and working copies directory")
	check := flag.Bool("check-config", false, "validate configuration and exit")
	flag.Parse()
	c, err := config.Load(*configPath)
	if err != nil {
		reportFailure(os.Stderr, "чтение и проверка конфигурации", "Конфигурация", *configPath, err)
		os.Exit(1)
	}
	gitClient := gitrepo.Client{}
	if err = gitClient.ValidateAll(context.Background(), c.Repositories); err != nil {
		reportFailure(os.Stderr, "проверка Git-репозиториев", "Конфигурация", *configPath, err)
		os.Exit(1)
	}
	engine := &pipeline.Engine{Git: gitClient, Definitions: pipeline.Builtins(), NewProvider: func(c config.Provider) (pipeline.Provider, error) { return inference.NewHTTP(c) }}
	for name, p := range c.Pipelines {
		if _, err = engine.Fields(p); err != nil {
			reportFailure(os.Stderr, "проверка pipeline "+name, "Конфигурация", *configPath, err)
			os.Exit(1)
		}
	}
	if *check {
		fmt.Println("Configuration valid")
		return
	}
	if *data == "" {
		root, e := os.UserConfigDir()
		if e != nil {
			reportFailure(os.Stderr, "определение каталога данных", "", "", e)
			os.Exit(1)
		}
		*data = filepath.Join(root, "slop-generator")
	}
	store, err := history.NewJSON[task.Task](filepath.Join(*data, "history.json"))
	if err != nil {
		reportFailure(os.Stderr, "подготовка хранилища истории", "Каталог данных", *data, err)
		os.Exit(1)
	}
	e, err := task.New(c, *data, task.Dependencies{
		Pipeline: engine, History: store,
	})
	if err != nil {
		reportFailure(os.Stderr, "загрузка истории и подготовка сервиса тасок", "История", filepath.Join(*data, "history.json"), err)
		os.Exit(1)
	}
	err = tui.Run(e)
	e.Close()
	if err != nil {
		reportFailure(os.Stderr, "инициализация или работа TUI", "", "", err)
		os.Exit(1)
	}
}
