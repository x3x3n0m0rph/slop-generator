package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"slop-generator/internal/config"
)

// reportFailure is presentation only; packages preserve typed causes for callers.
func reportFailure(w io.Writer, area, resourceLabel, resource string, err error) {
	fmt.Fprintf(w, "Ошибка запуска: %s\n", area)
	if resource != "" {
		if abs, e := filepath.Abs(resource); e == nil {
			resource = abs
		}
		fmt.Fprintf(w, "  %s: %s\n", resourceLabel, resource)
	}
	var field *config.FieldError
	if errors.As(err, &field) {
		fmt.Fprintf(w, "  Поле: %s\n", field.Field)
	}
	reason := err.Error()
	var path *config.PathError
	var osPath *os.PathError
	if errors.As(err, &path) {
		fmt.Fprintf(w, "  Путь: %s\n", path.Path)
		switch path.Problem {
		case "does not exist":
			reason = "Каталог не существует."
		case "is not a directory":
			reason = "Указан файл вместо каталога."
		case "access denied":
			reason = "Нет доступа к каталогу."
		default:
			reason = "Не удалось получить доступ к каталогу."
		}
	} else if errors.As(err, &osPath) {
		fmt.Fprintf(w, "  Путь: %s\n", osPath.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			reason = "Файл или каталог не существует."
		case errors.Is(err, os.ErrPermission):
			reason = "Недостаточно прав для доступа к файлу или каталогу."
		default:
			reason = "Не удалось выполнить операцию с файлом или каталогом: " + osPath.Err.Error()
		}
	} else if field != nil {
		reason = field.Err.Error()
	}
	fmt.Fprintf(w, "  Причина: %s\n", strings.ReplaceAll(reason, "\n", "\n           "))
	if field != nil && field.Hint != "" {
		fmt.Fprintf(w, "  Исправление: %s\n", field.Hint)
	}
}
