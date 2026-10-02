package web

import (
	"bufio"
	"crypto/subtle"

	"embed"
	"fmt"
	"html/template"
	"io"

	"rtue7/config"
	"rtue7/modbus_rtu"
	"rtue7/modbus_tcp"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

type WebServer struct {
	config *config.Config
	tmpl   *template.Template
	//static  fs.FS
}

func NewServer(cfg *config.Config) *WebServer {
	// Загрузка шаблонов
	//tmpl := template.Must(template.ParseGlob("web/templates/*.html"))
	tmpl := template.Must(template.ParseFS(tmplFS, "templates/*.html"))
	return &WebServer{
		config: cfg,
		tmpl:   tmpl,
	}
}

func (ws *WebServer) Start() error {
	http.HandleFunc("/", ws.index)
	http.HandleFunc("/config", ws.authMiddleware(ws.configHandler))
	http.HandleFunc("/motor", ws.motorHandler)
	//http.HandleFunc("/motoraxis", ws.changeAxis)
	http.HandleFunc("/registers", ws.authMiddleware(ws.registersHandler))
	http.HandleFunc("/video", ws.assunPage) 
	http.HandleFunc("/assun", ws.assunPage)
	http.HandleFunc("/video_feed", ws.authMiddleware(ws.videoFeed))

	// Статические файлы
	//http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	// Статика: отдаём содержимое static/ по префиксу /static/
	staticSub, _ := fs.Sub(staticFS, "static")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	log.Printf("Web server listening on :%d", ws.config.WebServerPort)
	return http.ListenAndServe(fmt.Sprintf(":%d", ws.config.WebServerPort), nil)
}

// BasicAuth middleware
func (ws *WebServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(ws.config.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		_ = user // не используется
		next(w, r)
	}
}

// Перенаправление на /motor
func (ws *WebServer) index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/motor", http.StatusFound)
}

// Страница конфигурации
func (ws *WebServer) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// Декодирование формы в структуру
		newCfg := &config.Config{}
		newCfg.Password = r.FormValue("password")
		newCfg.TCPServerPort, _ = strconv.Atoi(r.FormValue("tcp_port"))
		newCfg.WebServerPort, _ = strconv.Atoi(r.FormValue("web_port"))
		newCfg.ModbusRTU.Port = r.FormValue("rtu_port")
		newCfg.ModbusRTU.BaudRate, _ = strconv.Atoi(r.FormValue("baud"))
		newCfg.ModbusRTU.DataBits, _ = strconv.Atoi(r.FormValue("data_bits"))
		newCfg.ModbusRTU.StopBits, _ = strconv.Atoi(r.FormValue("stop_bits"))
		newCfg.ModbusRTU.Parity = r.FormValue("parity")
		newCfg.Camera.RTSPURL = r.FormValue("rtsp_url")
		newCfg.Camera.Username = r.FormValue("camera_user")
		newCfg.Camera.Password = r.FormValue("camera_pass")
		newCfg.Motors.AxisX, _ = strconv.Atoi(r.FormValue("axis_x"))
		newCfg.Motors.AxisY, _ = strconv.Atoi(r.FormValue("axis_y"))
		newCfg.Motors.AxisZ, _ = strconv.Atoi(r.FormValue("axis_z"))
		newCfg.Motors.AxisA, _ = strconv.Atoi(r.FormValue("axis_a"))
		gainX, _ := strconv.ParseFloat(r.FormValue("gain_x"), 32)
		gainY, _ := strconv.ParseFloat(r.FormValue("gain_y"), 32)
		gainZ, _ := strconv.ParseFloat(r.FormValue("gain_z"), 32)
		gainA, _ := strconv.ParseFloat(r.FormValue("gain_a"), 32)
		newCfg.Motors.GainX = float32(gainX)
		newCfg.Motors.GainY = float32(gainY)
		newCfg.Motors.GainZ = float32(gainZ)
		newCfg.Motors.GainA = float32(gainA)
		s, _ := strconv.Atoi(r.FormValue("reg_speed"))
		en, _ := strconv.Atoi(r.FormValue("reg_en"))
		pos, _ := strconv.Atoi(r.FormValue("reg_pos"))
		mod, _ := strconv.Atoi(r.FormValue("reg_mod"))
		gs, _ := strconv.Atoi(r.FormValue("reg_getspeed"))
		gp, _ := strconv.Atoi(r.FormValue("reg_getpos"))
		newCfg.Registers.Speed = uint16(s)
		newCfg.Registers.Enable = uint16(en)
		newCfg.Registers.Position = uint16(pos)
		newCfg.Registers.Mode = uint16(mod)
		newCfg.Registers.GetSpeed = uint16(gs)
		newCfg.Registers.GetPosition = uint16(gp)

		if err := config.Save(newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config?ok=1", http.StatusFound)
		// Инициализация Modbus RTU клиента
		if newCfg.ModbusRTU.Port != ws.config.ModbusRTU.Port ||
			newCfg.ModbusRTU.BaudRate != ws.config.ModbusRTU.BaudRate ||
			newCfg.ModbusRTU.DataBits != ws.config.ModbusRTU.DataBits ||
			newCfg.ModbusRTU.StopBits != ws.config.ModbusRTU.StopBits ||
			newCfg.ModbusRTU.Parity != ws.config.ModbusRTU.Parity {
			if err := modbus_rtu.Start(newCfg.ModbusRTU); err != nil {
				log.Println(err.Error())
			} else {
				log.Printf("Подключились к порту %s", newCfg.ModbusRTU.Port)
			}
		}
		//defer modbus_rtu.Close()

		// Запуск TCP Modbus сервера
		if newCfg.TCPServerPort != ws.config.TCPServerPort {
			if err := modbus_tcp.Start(newCfg.TCPServerPort); err != nil {
				log.Println(err.Error())
			} else {
				log.Printf("TCP Modbus server port: %v", newCfg.TCPServerPort)
			}
		}
		ws.config = newCfg
		return
	}
	ws.tmpl.ExecuteTemplate(w, "config.html", ws.config)
}

// Управление двигателями

func (ws *WebServer) motorHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"SelAxis":   "x",
		"speedH":    "500",
		"stepH":     "100",
		"direction": "",
		"message":   "ok",
		"ok":        1,
	}
	var err error
	defer func() {
		if err != nil {
			data["message"] = err.Error()
			data["ok"] = 0
			data["error"] = 1
		}
		ws.tmpl.ExecuteTemplate(w, "motor.html", data)
	}()
	if r.Method == http.MethodPost {
		r.ParseForm()
		axis := r.FormValue("axis")
		speedStr := r.FormValue("setspeed")
		posStr := r.FormValue("setpos")
		direction := r.FormValue("direction")
		data["SelAxis"] = axis
		data["speedH"] = speedStr
		data["stepH"] = posStr

		if direction == "" {
			data["message"] = "Nothing"
			return
		}

		// Определение slave ID по оси
		slaveID := uint8(1)
		var GainF float32
		switch axis {
		case "x":
			slaveID = uint8(ws.config.Motors.AxisX)
			GainF = (ws.config.Motors.GainX)
		case "y":
			slaveID = uint8(ws.config.Motors.AxisY)
			GainF = (ws.config.Motors.GainY)
		case "z":
			slaveID = uint8(ws.config.Motors.AxisZ)
			GainF = (ws.config.Motors.GainZ)
		case "a":
			slaveID = uint8(ws.config.Motors.AxisA)
			GainF = (ws.config.Motors.GainA)
		}

		if direction == "stop" {
			err = modbus_rtu.WriteSingleRegister(slaveID, ws.config.Registers.Enable, 1)
			if err == nil {
				data["message"] = "Drive stop - axis:" + axis
			}
			return
		}
		speed, _ := strconv.ParseFloat(speedStr, 32)
		if speed == 0 {
			err = fmt.Errorf("Not set Speed")
			return
		}
		t := false
		sp := uint32(float32(speed) * GainF)

		if direction == "backward" {
			t = true
			sp = -sp
		}
		if direction == "forward" || t {
			err = modbus_rtu.WriteSingleRegister(slaveID, ws.config.Registers.Enable, 10)
			if err == nil {
				err = modbus_rtu.WriteSingleRegister(slaveID, ws.config.Registers.Mode, 1)
				if err == nil {
					err = modbus_rtu.WriteDoubleRegister(slaveID, ws.config.Registers.Speed, sp)
					if err == nil {
						data["message"] = fmt.Sprintf("Drive %s Move %s, speed  %f", axis, direction, speed)
					}
				}
			}
			return
		}

		step, _ := strconv.ParseFloat(posStr, 32)
		st := int32(float32(step) * GainF)
		if st == 0 {
			err = fmt.Errorf("Not set Step")
			return
		}
		if direction == "backwardstep" {
			t = true
			st = -st
		}

		if direction == "forwardstep" || t {
			pos, e := modbus_rtu.ReadInt32Register(slaveID, ws.config.Registers.GetPosition)
			err = e
			pos += st
			if err == nil {
				err = modbus_rtu.WriteSingleRegister(slaveID, ws.config.Registers.Enable, 10)
				if err == nil {
					err = modbus_rtu.WriteSingleRegister(slaveID, ws.config.Registers.Mode, 0)
					if err == nil {
						err = modbus_rtu.WriteDoubleRegister(slaveID, ws.config.Registers.Position, uint32(pos))
						if err == nil {
							err = modbus_rtu.WriteDoubleRegister(slaveID, ws.config.Registers.Speed, sp)
							if err == nil {
								data["message"] = fmt.Sprintf("Drive %s Move %s, speed  %f, step %d, to position %d", axis, direction, speed, st, pos)
							}
						}
					}
				}
			}
			return
		}
		err = fmt.Errorf("Not set Command")
	}
}

// Страница чтения/записи регистров
func (ws *WebServer) registersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		sl, _ := strconv.ParseUint(r.FormValue("slave_id"), 10, 8)
		slaveID := uint8(sl)
		regType := r.FormValue("reg_type")
		// Address is entered in hexadecimal (e.g. 0x1234 or 1234)
		rawAddr := strings.TrimSpace(r.FormValue("address"))
		rawAddr = strings.TrimPrefix(strings.ToLower(rawAddr), "0x")
		address, _ := strconv.ParseUint(rawAddr, 16, 16)
		valueStr := r.FormValue("value")

		if r.FormValue("action") == "read" {
			var result interface{}
			var err error
			switch regType {
			case "holding":
				res, err := modbus_rtu.ReadHoldingRegisters(slaveID, uint16(address), 1)
				if err == nil && len(res) > 0 {
					result = int(res[0])*256 + int(res[1])
				}
			default:
				err = fmt.Errorf("unsupported register type for read")
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			ws.tmpl.ExecuteTemplate(w, "registers.html", map[string]interface{}{"result": result})
			return
		} else if r.FormValue("action") == "write" {
			value, _ := strconv.ParseUint(valueStr, 10, 16)
			var err error
			switch regType {
			case "holding":
				err = modbus_rtu.WriteSingleRegister(slaveID, uint16(address), uint16(value))
			default:
				err = fmt.Errorf("unsupported register type for write")
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			//http.Redirect(w, r, "/registers?ok=1", http.StatusFound)
			ws.tmpl.ExecuteTemplate(w, "registers.html", map[string]interface{}{"ok": 1})
			return
		}
	}
	ws.tmpl.ExecuteTemplate(w, "registers.html", nil)
}

// Страница ASSUN (бывшая Video) с вкладками второго уровня
func (ws *WebServer) assunPage(w http.ResponseWriter, r *http.Request) {
	ws.tmpl.ExecuteTemplate(w, "assun.html", nil)
}

// Страница видео
func (ws *WebServer) videoPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/assun", http.StatusFound)
	//ws.tmpl.ExecuteTemplate(w, "video.html", nil)
}

// MJPEG поток через ffmpeg
func (ws *WebServer) videoFeed(w http.ResponseWriter, r *http.Request) {
	rtspURL := ws.config.Camera.RTSPURL
	if ws.config.Camera.Username != "" && ws.config.Camera.Password != "" {
		// Встраиваем логин/пароль в URL (rtsp://user:pass@host/...)
		parts := strings.SplitN(rtspURL, "://", 2)
		if len(parts) == 2 {
			rtspURL = parts[0] + "://" + ws.config.Camera.Username + ":" + ws.config.Camera.Password + "@" + parts[1]
		}
	}

	cmd := exec.Command("ffmpeg",
		"-i", rtspURL,
		"-q:v", "5", // качество JPEG
		"-f", "mjpeg",
		"-")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stderr, _ := cmd.StderrPipe()
	go io.Copy(io.Discard, stderr) // игнорируем лог ffmpeg

	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cmd.Process.Kill()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	writer := bufio.NewWriter(w)
	boundary := "--frame\r\n"
	contentType := "Content-Type: image/jpeg\r\n\r\n"

	buf := make([]byte, 4096)
	for {
		// Ожидаем начало JPEG (0xFFD8)
		start := false
		var frame []byte
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			data := buf[:n]
			if !start {
				// Ищем маркер начала JPEG
				idx := indexJPEGStart(data)
				if idx >= 0 {
					frame = append(frame, data[idx:]...)
					start = true
				}
			} else {
				frame = append(frame, data...)
				// Ищем конец JPEG (0xFFD9)
				if idx := indexJPEGEnd(frame); idx >= 0 {
					frame = frame[:idx+2] // включая маркер конца
					break
				}
			}
		}
		// Отправляем кадр клиенту
		writer.WriteString(boundary)
		writer.WriteString(contentType)
		writer.Write(frame)
		writer.WriteString("\r\n")
		writer.Flush()
	}
}

// Поиск байтов 0xFF, 0xD8 (начало JPEG)
func indexJPEGStart(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0xD8 {
			return i
		}
	}
	return -1
}

// Поиск байтов 0xFF, 0xD9 (конец JPEG)
func indexJPEGEnd(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0xD9 {
			return i
		}
	}
	return -1
}
