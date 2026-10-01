package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"slop-generator/internal/config"
)

func TestStartupRepositoryErrorPresentation(t *testing.T) {
	cause := errors.New("GetFileAttributesEx: platform internals")
	path := &config.PathError{Path: `C:\DEV\playground`, Problem: "does not exist", Cause: cause}
	err := config.At("repositories.playground.path", path, "Укажите существующий репозиторий.")
	var out bytes.Buffer
	reportFailure(&out, "чтение и проверка конфигурации", "Конфигурация", "config.yaml", err)
	for _, want := range []string{"Ошибка запуска: чтение и проверка конфигурации", "Конфигурация:", "Поле: repositories.playground.path", `Путь: C:\DEV\playground`, "Причина: Каталог не существует.", "Исправление:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "GetFileAttributesEx") || strings.Contains(out.String(), `C:\\DEV`) {
		t.Fatal("OS details or escaped path leaked into output")
	}
	if !errors.Is(err, cause) {
		t.Fatal("underlying error lost")
	}
}

func TestStartupAreasAndFileErrors(t *testing.T) {
	for _, area := range []string{"проверка Git-репозиториев", "подготовка хранилища истории", "загрузка истории и подготовка сервиса тасок", "инициализация или работа TUI"} {
		var out bytes.Buffer
		reportFailure(&out, area, "История", "history.json", &os.PathError{Op: "open", Path: "history.json", Err: os.ErrPermission})
		if !strings.Contains(out.String(), "Ошибка запуска: "+area) || !strings.Contains(out.String(), "Недостаточно прав") || strings.Contains(out.String(), "permission denied") {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	reportFailure(&out, "чтение и проверка конфигурации", "Конфигурация", "missing.yaml", &os.PathError{Op: "open", Path: "missing.yaml", Err: os.ErrNotExist})
	if !strings.Contains(out.String(), "Файл или каталог не существует.") {
		t.Fatal(out.String())
	}
}
