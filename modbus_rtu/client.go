package modbus_rtu

import (
	"fmt"
	"log"
	"sync"
	"time"

	"rtue7/config"

	"github.com/goburrow/modbus"
)

var (
	handler *modbus.RTUClientHandler
	client  modbus.Client
	mutex   sync.Mutex
)

// Start создаёт подключение к последовательному порту и инициализирует Modbus RTU клиента.
func Start(cfg config.ModbusRTUConfig) error {
	Close()
	mutex.Lock()
	defer mutex.Unlock()
	h := modbus.NewRTUClientHandler(cfg.Port)
	h.BaudRate = cfg.BaudRate
	h.DataBits = cfg.DataBits
	h.StopBits = cfg.StopBits
	h.Parity = cfg.Parity
	h.Timeout = 1 * time.Second
	h.IdleTimeout = 1 * time.Second
	handler = h
	client = modbus.NewClient(handler)

	err := h.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to serial port: %v", err)
	}
	return nil //Connect() подключаться только после обращения чт
}

// Close закрывает соединение с портом.
func Close() {
	mutex.Lock()
	defer mutex.Unlock()
	log.Printf("Modbus_close")
	client = nil
	if handler != nil {
		handler.Close()
		handler = nil
	}
}

// Чтение holding регистров
func ReadHoldingRegisters(unitID uint8, address, quantity uint16) ([]byte, error) {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	results, err := client.ReadHoldingRegisters(address, quantity)
	if err != nil {
		log.Printf("Ошибка чтения регистров: %v", err)
		return nil, err
	}
	return results, nil
}
func ReadInt32Register(unitID uint8, address uint16) (int32, error) {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	r, err := client.ReadHoldingRegisters(address, 2)
	if err != nil {
		log.Printf("Ошибка чтения регистров: %v", err)
		return 0, err
	}
	var ret int32
	ret = (int32(r[0]) << 8) | (int32(r[1]) << 0) | (int32(r[2]) << 24) | (int32(r[3]) << 16)
	return ret, nil
}

// Запись одного регистра
func WriteSingleRegister(unitID uint8, address, value uint16) error {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	_, err := client.WriteSingleRegister(address, value)
	if err != nil {
		log.Printf("Ошибка записи регистра: %v", err)
		return err
	}
	return nil
}
func WriteDoubleRegister(unitID uint8, address uint16, value uint32) error {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	value1 := []byte{
		byte(value >> 8),
		byte(value >> 0),
		byte(value >> 24),
		byte(value >> 16),
	}
	_, err := client.WriteMultipleRegisters(address, 2, value1)
	if err != nil {
		log.Printf("Ошибка записи регистра: %v", err)
		return err
	}
	return nil
}

// Запись нескольких регистров
func WriteMultipleRegisters(unitID uint8, address, quantity uint16, value []byte) error {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	_, err := client.WriteMultipleRegisters(address, quantity, value)
	return err
}

// Чтение входных регистров
func ReadInputRegisters(unitID uint8, address, quantity uint16) ([]byte, error) {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	return client.ReadInputRegisters(address, quantity)
}

// Чтение
func ReadCoils(unitID uint8, address, quantity uint16) ([]byte, error) {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	return client.ReadCoils(address, quantity)
}

// Запись одного бита
func WriteSingleCoil(unitID uint8, address, value uint16) error {
	mutex.Lock()
	defer mutex.Unlock()
	handler.SlaveId = unitID
	_, err := client.WriteSingleCoil(address, value)
	return err
}
