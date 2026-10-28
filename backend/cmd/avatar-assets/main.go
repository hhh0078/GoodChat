// 文件作用说明：通过生产头像规范化流程处理内置素材，生成最长256像素的轻量PNG，原始生成素材由图像工具保留。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"supportdesk/backend/internal/chat"
)

// main 逐个校验50个独立头像并原子保存规范化结果，缺图或损坏时明确失败。
func main() {
	if len(os.Args) != 2 {
		log.Fatal("请传入包含default-01.png至default-50.png的素材目录")
	}
	var total int
	for index := 1; index <= 50; index++ {
		path := filepath.Join(os.Args[1], fmt.Sprintf("default-%02d.png", index))
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("读取头像%02d失败", index)
		}
		data, width, height, err := chat.NormalizeAvatar(raw)
		if err != nil {
			log.Fatalf("头像%02d规范化失败：%v", index, err)
		}
		if err = os.WriteFile(path+".normalized", data, 0644); err != nil {
			log.Fatalf("头像%02d保存失败", index)
		}
		if err = os.Rename(path+".normalized", path); err != nil {
			log.Fatalf("头像%02d替换失败", index)
		}
		total += len(data)
		fmt.Printf("头像%02d：%dx%d，%d字节\n", index, width, height, len(data))
	}
	fmt.Printf("已规范化50个头像，总计%d字节\n", total)
}
