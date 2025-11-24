package utils

import (
	"math/rand"
	"time"
)

func Shuffle[T any](arr []T) {
	rand.New(rand.NewSource(time.Now().UnixNano())).Shuffle(len(arr), func(i, j int) {
		arr[i], arr[j] = arr[j], arr[i]
	})
}
