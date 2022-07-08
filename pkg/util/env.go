package util

import (
	"os"
	"strconv"
)

func GetEnv(key string, defaultValue string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	return val
}

func GetEnvInt(key string, defaultValue int) int {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return v
}

func GetEnvBool(key string, defaultValue bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	v, err := strconv.ParseBool(val)
	if err != nil {
		return defaultValue
	}
	return v
}
