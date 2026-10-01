package config

import (
	"encoding/json"
	"log"
	"os"
)

var cfgFile string

type ModbusRTUConfig struct {
	Port     string `json:"port"`
	BaudRate int    `json:"baud_rate"`
	DataBits int    `json:"data_bits"`
	StopBits int    `json:"stop_bits"`
	Parity   string `json:"parity"` // "N", "E", "O"
}

type CameraConfig struct {
	RTSPURL  string `json:"rtsp_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type MotorsConfig struct {
	AxisX int     `json:"axis_x"`
	AxisY int     `json:"axis_y"`
	AxisZ int     `json:"axis_z"`
	AxisA int     `json:"axis_a"`
	GainX float32 `json:"gain_x"`
	GainY float32 `json:"gain_y"`
	GainZ float32 `json:"gain_z"`
	GainA float32 `json:"gain_a"`
}

type RegistersConfig struct {
	Speed       uint16 `json:"speed"`       // адрес holding регистра скорости 32bit
	Enable      uint16 `json:"enable"`      // адрес holding регистра Enable (10=on,1=off) 16bit
	Position    uint16 `json:"position"`    // адрес holding регистра position 32bit
	Mode        uint16 `json:"mode"`        // адрес holding регистра mode 0-position 1-speed 16bit
	GetSpeed    uint16 `json:"getspeed"`    // адрес ro регистра мгновенной скорости 32bit
	GetPosition uint16 `json:"getposition"` // адрес ro регистра position 32bit

}
type Config struct {
	Password      string          `json:"password"`
	TCPServerPort int             `json:"tcp_server_port"`
	WebServerPort int             `json:"web_server_port"`
	ModbusRTU     ModbusRTUConfig `json:"modbus_rtu"`
	Camera        CameraConfig    `json:"camera"`
	Motors        MotorsConfig    `json:"motors"`
	Registers     RegistersConfig `json:"modbus_registers"`
}

func defaultConfig() *Config {
	return &Config{
		Password:      "admin",
		TCPServerPort: 502,
		WebServerPort: 1111,
		ModbusRTU: ModbusRTUConfig{
			Port:     "/dev/ttyS1",
			BaudRate: 230400,
			DataBits: 8,
			StopBits: 1,
			Parity:   "N",
		},
		Camera: CameraConfig{
			RTSPURL:  "",
			Username: "",
			Password: "",
		},
		Motors: MotorsConfig{
			AxisX: 1,
			AxisY: 2,
			AxisZ: 3,
			AxisA: 4,
			GainX: 1,
			GainY: 1,
			GainZ: 1,
			GainA: 1,
		},
		Registers: RegistersConfig{
			Speed:       0x1002,
			Enable:      0x1000,
			Position:    0x1008,
			Mode:        0x100A,
			GetSpeed:    0x1306,
			GetPosition: 0x1308,
		},
	}
}

// Load загружает конфигурацию из файла, если файла нет – создаёт с настройками по умолчанию.
func Load(path string) (*Config, error) {
	cfgFile = path
	data, err := os.ReadFile(path)
	log.Printf("Загрузка %s <<<", path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			if err := save(cfg, path); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save сохраняет конфигурацию в файл.
func Save(cfg *Config) error {
	return save(cfg, cfgFile)
}

func save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	log.Printf("Сохранение %s <<<", path)
	return os.WriteFile(path, data, 0644)
}
