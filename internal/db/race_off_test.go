//go:build !race

package db

// raceDetectorEnabled 表示当前测试二进制未启用竞态检测，重型故障注入遍历照常执行。
const raceDetectorEnabled = false
