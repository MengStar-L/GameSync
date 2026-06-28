//go:build windows

package core

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectSecret(value string) (string, error) {
	data := []byte(value)
	if len(data) == 0 {
		return "", nil
	}
	input := windows.DataBlob{
		Size: uint32(len(data)),
		Data: &data[0],
	}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	protected := unsafe.Slice(output.Data, output.Size)
	return base64.StdEncoding.EncodeToString(protected), nil
}

func unprotectSecret(value string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	input := windows.DataBlob{
		Size: uint32(len(data)),
		Data: &data[0],
	}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	plaintext := unsafe.Slice(output.Data, output.Size)
	return string(plaintext), nil
}
