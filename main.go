package main

import (
	"log"
	"rtue7/config"
	"rtue7/modbus_rtu"
	"rtue7/modbus_tcp"
	"rtue7/web"
	"runtime"
)

func main() {
	// Загрузка конфигурации
	cfgstr := "asot.json"
	if runtime.GOOS != "windows" {
		cfgstr = "/etc/asot.json"
	}
	cfg, err := config.Load(cfgstr)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	InitRtuTCP(cfg)
	defer modbus_rtu.Close()
	defer modbus_tcp.Close()
	// Запуск веб-сервера
	webServer := web.NewServer(cfg)
	if err := webServer.Start(); err != nil {
		log.Fatalf("Web server error: %v", err)
	}
}

func InitRtuTCP(cfg *config.Config) {

	// Инициализация Modbus RTU клиента
	if err := modbus_rtu.Start(cfg.ModbusRTU); err != nil {
		log.Println(err.Error())
	} else {
		log.Printf("Подключились к порту %s", cfg.ModbusRTU.Port)
	}

	// Запуск TCP Modbus сервера
	if err := modbus_tcp.Start(cfg.TCPServerPort); err != nil {
		log.Println(err.Error())
	} else {
		log.Printf("TCP Modbus server port: %v", cfg.TCPServerPort)
	}

}
