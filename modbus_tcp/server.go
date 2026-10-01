package modbus_tcp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"rtue7/modbus_rtu"
)

var (
	port       int
	listener   net.Listener
	stopServer chan struct{}
)

type ModbusRequest struct {
	TransactionID uint16
	ProtocolID    uint16
	Length        uint16
	UnitID        uint8
	FunctionCode  uint8
	Data          []byte
}

func Start(prt int) error {
	if prt == port {
		return nil
	}
	Close()

	lst, err := net.Listen("tcp", fmt.Sprintf(":%d", prt))
	if err != nil {
		return fmt.Errorf("ошибка запуска TCP сервера: %v", err)
	}
	port = prt
	listener = lst

	log.Printf("TCP сервер запущен на порту %d", port)
	stopServer = make(chan struct{})
	//defer close(stopServer)
	go func(done chan struct{}) {
		for {
			select {
			case <-done:
				//fmt.Println("Горутина получила сигнал остановки и завершает работу.")
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					log.Printf("Ошибка принятия соединения: %v", err)
					continue
				}
				log.Printf("Подключен: %s", net.Conn.RemoteAddr(conn).String())
				go handleConnection(conn, stopServer)
			}
		}
	}(stopServer)
	return nil
}

func Close() {
	if port != 0 { //закрыть предыдущее сокдинение
		close(stopServer)
		listener.Close()
		port = 0
		listener = nil
	}
}

func handleConnection(conn net.Conn, done chan struct{}) {
	defer conn.Close()
	for {
		select {
		case <-done:
			//fmt.Println("Горутина получила сигнал остановки и завершает работу.")
			return
		default:
			// Чтение Modbus TCP заголовка (7 байт)
			header := make([]byte, 7)
			_, err := conn.Read(header)
			if err != nil {
				return
			}

			// Парсинг заголовка
			transactionID := binary.BigEndian.Uint16(header[0:2])
			protocolID := binary.BigEndian.Uint16(header[2:4])
			length := binary.BigEndian.Uint16(header[4:6])
			unitID := header[6]

			// Чтение PDU (длина - 1 байт на unitID)
			pduLength := length - 1
			pdu := make([]byte, pduLength)
			_, err = conn.Read(pdu)
			if err != nil {
				return
			}

			// Обработка запроса
			response := processModbusRequest(unitID, pdu)

			// Формирование ответа
			respHeader := make([]byte, 8)
			binary.BigEndian.PutUint16(respHeader[0:2], transactionID)
			binary.BigEndian.PutUint16(respHeader[2:4], protocolID)
			binary.BigEndian.PutUint16(respHeader[4:6], uint16(len(response)+1))
			respHeader[6] = unitID
			respHeader[7] = response[0] // Код функции

			// Отправка ответа
			fullResponse := append(respHeader, response[1:]...)
			conn.Write(fullResponse)
		}
	}
}

func processModbusRequest(unitID uint8, pdu []byte) []byte {
	if len(pdu) < 1 {
		return errorResponse(0x01, 0x02) // Недопустимые данные
	}

	//modbus_rtu.SetSlave(unitID) //номер устройства modbus

	functionCode := pdu[0]

	switch functionCode {
	case 0x03: // Read Holding Registers
		if len(pdu) < 5 {
			return errorResponse(functionCode, 0x02)
		}
		address := binary.BigEndian.Uint16(pdu[1:3])
		quantity := binary.BigEndian.Uint16(pdu[3:5])
		//if quantity == 9 {
		//	return errorResponse(functionCode, 0x03)
		//}

		if quantity < 1 || quantity > 125 {
			return errorResponse(functionCode, 0x03)
		}

		data, err := modbus_rtu.ReadHoldingRegisters(unitID, address, quantity)
		if err != nil {
			data, err = modbus_rtu.ReadHoldingRegisters(unitID, address, quantity)
			if err != nil {
				return errorResponse(functionCode, 0x04)
			}
		}

		response := []byte{functionCode, byte(len(data))}
		response = append(response, data...)
		return response

	case 0x06: // Write Single Register
		if len(pdu) < 5 {
			return errorResponse(functionCode, 0x02)
		}
		address := binary.BigEndian.Uint16(pdu[1:3])
		value := binary.BigEndian.Uint16(pdu[3:5])

		err := modbus_rtu.WriteSingleRegister(unitID, address, value)
		if err != nil {
			err := modbus_rtu.WriteSingleRegister(unitID, address, value)
			if err != nil {
				return errorResponse(functionCode, 0x04)
			}
		}

		return pdu[:5] // Эхо запроса

	case 0x10: // Write Multiple Registers
		if len(pdu) < 7 {
			return errorResponse(functionCode, 0x02)
		}
		address := binary.BigEndian.Uint16(pdu[1:3])
		quantity := binary.BigEndian.Uint16(pdu[3:5])
		byteCount := pdu[5]

		if len(pdu) < 6+int(byteCount) {
			return errorResponse(functionCode, 0x02)
		}

		value := pdu[6 : 6+byteCount]
		err := modbus_rtu.WriteMultipleRegisters(unitID, address, quantity, value)
		if err != nil {
			err = modbus_rtu.WriteMultipleRegisters(unitID, address, quantity, value)
			if err != nil {
				return errorResponse(functionCode, 0x04)
			}
		}

		return pdu[:5]

	default:
		return errorResponse(functionCode, 0x01) // Неподдерживаемая функция
	}
}

func errorResponse(functionCode byte, exceptionCode byte) []byte {
	return []byte{functionCode | 0x80, exceptionCode}
}

func Stop() {
	if listener != nil {
		listener.Close()
	}
}

// Преобразование hex строки в байты
func hexToBytes(hexStr string) ([]byte, error) {
	return hex.DecodeString(hexStr)
}

/*
import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"rtue7/modbus_rtu"
)

type Server struct {
	port int
}

func NewServer(port int) *Server {
	return &Server{port: port}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("Modbus TCP server listening on port %d", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		// Чтение MBAP заголовка (7 байт)
		header := make([]byte, 7)
		if _, err := io.ReadFull(conn, header); err != nil {
			if err != io.EOF {
				log.Printf("Read header error: %v", err)
			}
			return
		}
		transactionID := binary.BigEndian.Uint16(header[0:2])
		protocolID := binary.BigEndian.Uint16(header[2:4])
		length := binary.BigEndian.Uint16(header[4:6])
		unitID := header[6]

		// Проверка протокола (должен быть 0)
		if protocolID != 0 {
			log.Printf("Unsupported protocol ID: %d", protocolID)
			return
		}
		// Чтение PDU (length - 1 байт, т.к. unit ID уже считан)
		pduLen := int(length) - 1
		if pduLen < 0 {
			log.Printf("Invalid length: %d", length)
			return
		}
		pdu := make([]byte, pduLen)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			log.Printf("Read PDU error: %v", err)
			return
		}

		// Отправка запроса через Modbus RTU
		respPDU, err := modbus_rtu.SendRawRequest(unitID, pdu)
		if err != nil {
			log.Printf("RTU request error: %v", err)
			// Формируем исключение Modbus (код ошибки 0x80 + код функции)
			if len(pdu) > 0 {
				errorPDU := []byte{pdu[0] | 0x80, 0x01} // исключение "Illegal function"
				sendTCPResponse(conn, transactionID, unitID, errorPDU)
			}
			continue
		}

		// Отправка ответа обратно по TCP
		sendTCPResponse(conn, transactionID, unitID, respPDU)
	}
}

func (s *Server) sendTCPResponse(conn net.Conn, transactionID uint16, unitID byte, respPDU []byte) {
	// Формируем MBAP: transactionID, protocolID=0, length = 1 + len(respPDU), unitID, затем PDU
	length := uint16(1 + len(respPDU))
	response := make([]byte, 7+len(respPDU))
	binary.BigEndian.PutUint16(response[0:2], transactionID)
	binary.BigEndian.PutUint16(response[2:4], 0)
	binary.BigEndian.PutUint16(response[4:6], length)
	response[6] = unitID
	copy(response[7:], respPDU)

	if _, err := conn.Write(response); err != nil {
		log.Printf("Write TCP response error: %v", err)
	}
}
*/
