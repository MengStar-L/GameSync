package core

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	credentialKDF        = "argon2id"
	credentialKDFTime    = uint32(1)
	credentialKDFMemory  = uint32(64 * 1024)
	credentialKDFThreads = uint8(4)
	credentialKeyLength  = uint32(32)
)

type accountCredentials struct {
	APIToken          string `json:"apiToken"`
	R2AccessKeyID     string `json:"r2AccessKeyId"`
	R2SecretAccessKey string `json:"r2SecretAccessKey"`
	WebdavPassword    string `json:"webdavPassword,omitempty"`
}

func EncryptAccountCredentials(account CloudflareAccount, password string) (EncryptedCredentialBlob, error) {
	if password == "" {
		return EncryptedCredentialBlob{}, errors.New("recovery password is empty")
	}

	salt, err := randomBytes(16)
	if err != nil {
		return EncryptedCredentialBlob{}, err
	}
	nonce, err := randomBytes(12)
	if err != nil {
		return EncryptedCredentialBlob{}, err
	}

	key := deriveCredentialKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedCredentialBlob{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedCredentialBlob{}, err
	}

	payload, err := json.Marshal(accountCredentials{
		APIToken:          account.APIToken,
		R2AccessKeyID:     account.R2AccessKeyID,
		R2SecretAccessKey: account.R2SecretAccessKey,
		WebdavPassword:    account.WebdavPassword,
	})
	if err != nil {
		return EncryptedCredentialBlob{}, err
	}

	return EncryptedCredentialBlob{
		Version:    1,
		KDF:        credentialKDF,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, payload, []byte(account.ID))),
	}, nil
}

func DecryptAccountCredentials(account CloudflareAccount, blob EncryptedCredentialBlob, password string) (CloudflareAccount, error) {
	if password == "" {
		return account, errors.New("recovery password is empty")
	}
	if blob.KDF != credentialKDF || blob.Version != 1 {
		return account, errors.New("unsupported credential package")
	}

	salt, err := base64.StdEncoding.DecodeString(blob.Salt)
	if err != nil {
		return account, err
	}
	nonce, err := base64.StdEncoding.DecodeString(blob.Nonce)
	if err != nil {
		return account, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(blob.Ciphertext)
	if err != nil {
		return account, err
	}

	key := deriveCredentialKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return account, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return account, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(account.ID))
	if err != nil {
		return account, err
	}

	var credentials accountCredentials
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return account, err
	}
	account.APIToken = credentials.APIToken
	account.R2AccessKeyID = credentials.R2AccessKeyID
	account.R2SecretAccessKey = credentials.R2SecretAccessKey
	// 旧备份包无 webdavPassword 字段时保留账号原值，避免解密把已有密码清空
	if credentials.WebdavPassword != "" {
		account.WebdavPassword = credentials.WebdavPassword
	}
	return account, nil
}

func deriveCredentialKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, credentialKDFTime, credentialKDFMemory, credentialKDFThreads, credentialKeyLength)
}

func randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(cryptorand.Reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
