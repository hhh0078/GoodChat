// Package sound 播放随包携带的固定提示音，后台提醒不依赖网页音频上下文。
package sound

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// Player 将固定PCM提示音按成员音量生成本机文件，不下载或执行远端声音资源。
type Player struct {
	mu     sync.Mutex
	dir    string
	assets fs.FS
}

// New 创建固定资源播放器，所有输出均限制在应用配置目录。
func New(dir string, assets fs.FS) *Player {
	return &Player{dir: filepath.Join(dir, "sounds"), assets: assets}
}

// Play 缩放PCM音量后交给系统音频API，支持界面隐藏时播放。
func (p *Player) Play(id string, volume float64) error {
	if volume <= 0 {
		return nil
	}
	volume = math.Min(1, volume)
	switch id {
	case "gentle", "glass", "bell", "wood", "bubble":
	default:
		id = "gentle"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(p.dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(p.dir, fmt.Sprintf("%s-%d.wav", id, int(math.Round(volume*100))))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		raw, err := fs.ReadFile(p.assets, "sounds/"+id+".wav")
		if err != nil {
			return err
		}
		if err = scalePCM(raw, volume); err != nil {
			return err
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			return err
		}
	}
	return playFile(path)
}

// scalePCM 仅接受固定16位PCM WAV并缩放样本，不改变系统或其他程序的音量。
func scalePCM(raw []byte, volume float64) error {
	if len(raw) < 44 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return errors.New("提示音格式无效")
	}
	valid := false
	for position := 12; position+8 <= len(raw); {
		size := int(binary.LittleEndian.Uint32(raw[position+4 : position+8]))
		start := position + 8
		if size < 0 || start+size > len(raw) {
			return errors.New("提示音长度无效")
		}
		if string(raw[position:position+4]) == "fmt " {
			valid = size >= 16 && binary.LittleEndian.Uint16(raw[start:start+2]) == 1 && binary.LittleEndian.Uint16(raw[start+14:start+16]) == 16
		}
		if string(raw[position:position+4]) == "data" {
			if !valid || size%2 != 0 {
				return errors.New("提示音必须使用16位PCM")
			}
			for offset := start; offset+2 <= start+size; offset += 2 {
				value := int16(binary.LittleEndian.Uint16(raw[offset : offset+2]))
				scaled := int16(math.Round(float64(value) * volume))
				binary.LittleEndian.PutUint16(raw[offset:offset+2], uint16(scaled))
			}
			return nil
		}
		position = start + size + size%2
	}
	return errors.New("提示音缺少音频数据")
}
