package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"motty/internal/backup"
	"motty/internal/workbench"
)

func runSettingsBackup(args []string) error {
	if len(args) != 3 || (args[0] != "export" && args[0] != "check" && args[0] != "verify" && args[0] != "restore") {
		return errors.New("usage: motty settings-backup export|check|verify|restore DATABASE ENCRYPTED-FILE (recovery key on stdin)")
	}
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 4097))
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	key := strings.TrimRight(line, "\r\n")
	if len(key) < backup.MinPasswordLength || len(key) > 4096 {
		return errors.New("invalid recovery key")
	}
	var sealed []byte
	if args[0] != "export" {
		file, err := os.Open(args[2])
		if err != nil {
			return err
		}
		defer file.Close()
		sealed, err = io.ReadAll(io.LimitReader(file, backup.MaxSize+(1<<16)+1))
		if err != nil {
			return err
		}
	}
	result, message, err := workbench.SettingsBackup(args[0], args[1], sealed, key)
	if err != nil {
		return err
	}
	if args[0] == "export" {
		file, err := os.OpenFile(args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(result)
		return errors.Join(err, file.Close())
	}
	fmt.Println(message)
	return nil
}
