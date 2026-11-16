// 文件作用说明：为Windows可执行文件生成版本、品牌图标与高DPI兼容资源，支持在Mac交叉构建。
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// main 生成目标架构专用资源对象，不把Windows构建工具链加入客户端运行依赖。
func main() {
	arch := flag.String("arch", "amd64", "目标架构")
	versionValue := flag.String("version", "0.1.0", "版本号")
	iconPath := flag.String("icon", "icon.ico", "品牌图标")
	output := flag.String("out", "rsrc_windows_amd64.syso", "资源对象路径")
	flag.Parse()
	resources := winres.ResourceSet{}
	file, err := os.Open(*iconPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	icon, err := winres.LoadICO(file)
	if err != nil {
		log.Fatal(err)
	}
	if err = resources.SetIcon(winres.RT_ICON, icon); err != nil {
		log.Fatal(err)
	}
	resources.SetManifest(winres.AppManifest{ExecutionLevel: winres.AsInvoker, DPIAwareness: winres.DPIPerMonitorV2, UseCommonControlsV6: true})
	raw, _ := json.Marshal(map[string]any{"fixed": map[string]string{"file_version": *versionValue}, "info": map[string]any{"0804": map[string]string{"ProductVersion": *versionValue, "FileVersion": *versionValue, "CompanyName": "GoodChat", "FileDescription": "GoodChat 客服工作台", "ProductName": "GoodChat", "OriginalFilename": "GoodChat.exe"}}})
	var info version.Info
	if err = info.UnmarshalJSON(raw); err != nil {
		log.Fatal(err)
	}
	resources.SetVersionInfo(info)
	target := winres.ArchAMD64
	if *arch == "arm64" {
		target = winres.ArchARM64
	} else if *arch != "amd64" {
		log.Fatal("不支持的目标架构")
	}
	f, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	if err = resources.WriteObject(f, target); err != nil {
		f.Close()
		log.Fatal(err)
	}
	if err = f.Close(); err != nil {
		log.Fatal(err)
	}
}
