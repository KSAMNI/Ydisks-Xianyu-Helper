//go:build race

package db

// raceDetectorEnabled 表示当前测试二进制启用了竞态检测；重型故障注入遍历在该模式下跳过以控制耗时。
const raceDetectorEnabled = true
