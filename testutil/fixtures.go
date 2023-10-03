package testutil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type FixtureType string

const (
	FixtureTypeRequest  FixtureType = "requests"
	FixtureTypeManifest FixtureType = "manifests"
	FixtureTypeMedia    FixtureType = "media"
	FixtureTypeResponse FixtureType = "responses"

	fixtureDirName = "fixtures"

	UUIDV4Length = 36
)

func GetFixture(t *testing.T, typeName FixtureType, name string) []byte {
	t.Helper()

	fixturePath := GetFixturePath(t, typeName, name)

	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	return data
}

func GetFixtureAsType(t *testing.T, typeName FixtureType, name string, v interface{}) {
	t.Helper()

	data := GetFixture(t, typeName, name)
	require.NoError(t, json.Unmarshal(data, v))
}

func GetFixturePath(t *testing.T, typeName FixtureType, name string) string {
	t.Helper()

	_, currpath, _, ok := runtime.Caller(0)
	curdir := path.Dir(currpath)
	require.True(t, ok, "could not get current path")

	return path.Join(curdir, "..", fixtureDirName, string(typeName), name)
}

func RandomString(t *testing.T, length int) string {
	t.Helper()

	b := make([]byte, length)
	_, err := rand.Read(b)
	require.NoError(t, err)

	return base64.URLEncoding.EncodeToString(b)
}
