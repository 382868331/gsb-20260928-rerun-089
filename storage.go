package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// loadStoredProgram 从磁盘恢复上次保存的程序；文件不存在时返回内置示例。
// 保存的程序同样必须通过 compileProgram 的完整校验，损坏或非法文件不会被静默使用。
func loadStoredProgram(path string) (Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return demoProgram(), nil
		}
		return Program{}, err
	}
	var program Program
	if err := json.Unmarshal(data, &program); err != nil {
		return Program{}, err
	}
	if _, err := compileProgram(program); err != nil {
		return Program{}, err
	}
	return program, nil
}

// saveProgram 原子写入程序文件，避免写入中途崩溃产生半截文件。
func saveProgram(path string, program Program) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".program-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
