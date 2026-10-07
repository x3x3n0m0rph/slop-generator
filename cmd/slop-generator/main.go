package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"slop-generator/internal/config"
	"slop-generator/internal/gitrepo"
	"slop-generator/internal/history"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
	"slop-generator/internal/task"
	"slop-generator/internal/tui"
)

type oauthClientKey struct {
	Settings           config.OAuth2
	SOCKS5, SOCKS5File string
}

func oauthKey(provider config.Provider) oauthClientKey {
	return oauthClientKey{Settings: provider.OAuth2, SOCKS5: provider.SOCKS5, SOCKS5File: provider.SOCKS5File}
}

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
	engine := &pipeline.Engine{Git: gitClient, Definitions: pipeline.Builtins()}
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
	oauthManagers := map[oauthClientKey]*inference.OAuthManager{}
	for _, name := range slices.Sorted(maps.Keys(c.Providers)) {
		provider := c.Providers[name]
		if provider.AuthMode != "oauth2" {
			continue
		}
		key := oauthKey(provider)
		if oauthManagers[key] != nil {
			continue
		}
		profileHash := sha256.Sum256([]byte(fmt.Sprintf("%s\n%#v", name, provider.OAuth2)))
		tokenPath := filepath.Join(*data, "oauth2", hex.EncodeToString(profileHash[:])+".json")
		manager, managerErr := inference.NewOAuthManager(context.Background(), provider, tokenPath)
		if managerErr != nil {
			for _, initialized := range oauthManagers {
				initialized.Close()
			}
			reportFailure(os.Stderr, "OAuth2-аутентификация провайдера "+name, "OAuth2", provider.OAuth2.WellKnownURL, managerErr)
			os.Exit(1)
		}
		oauthManagers[key] = manager
	}
	closeOAuthManagers := func() {
		for _, manager := range oauthManagers {
			manager.Close()
		}
	}
	store, err := history.NewJSON[task.Task](filepath.Join(*data, "history.json"))
	if err != nil {
		closeOAuthManagers()
		reportFailure(os.Stderr, "подготовка хранилища истории", "Каталог данных", *data, err)
		os.Exit(1)
	}
	engine.NewProvider = func(provider config.Provider) (pipeline.Provider, error) {
		if provider.AuthMode == "oauth2" {
			manager := oauthManagers[oauthKey(provider)]
			if manager == nil {
				return nil, fmt.Errorf("OAuth2 provider was not initialized at startup")
			}
			return manager.Session(provider)
		}
		return inference.NewHTTP(provider)
	}
	e, err := task.New(c, *data, task.Dependencies{Pipeline: engine, History: store})
	if err != nil {
		closeOAuthManagers()
		reportFailure(os.Stderr, "загрузка истории и подготовка сервиса тасок", "История", filepath.Join(*data, "history.json"), err)
		os.Exit(1)
	}
	err = tui.Run(e)
	e.Close()
	closeOAuthManagers()
	if err != nil {
		reportFailure(os.Stderr, "инициализация или работа TUI", "", "", err)
		os.Exit(1)
	}
}
