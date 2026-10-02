package web

import (
	"bufio"
	"crypto/subtle"

	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"

	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"rtue7/config"
	"rtue7/modbus_rtu"
	"rtue7/modbus_tcp"
	"strconv"
	"strings"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed data/*.csv
var dataFS embed.FS

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
	http.HandleFunc("/api/parameters", ws.apiParameters)
	http.HandleFunc("/api/write", ws.authMiddleware(ws.apiWriteParameter))
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

// parseAssunCSV разбирает CSV-файл с разделителем '@' в список параметров.
// Формат строк (по одной записи на строку):
// Classify@Address@Name@Value@Unit@DataType@Notes@Display mode@Decimal place
// Разбор идёт построчно: переводы строк внутри полей не поддерживаются,
// что исключает рассинхронизацию разбивки при лишних/недостающих '@' в строке.
func parseAssunCSV(content string) []map[string]string {
	trim := func(s string) string {
		s = strings.TrimSpace(s)
		// снимаем окружающие двойные кавычки, если поле заключено в них
		if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
			s = strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
		}
		return s
	}

	var params []map[string]string
	// Ожидаемое число полей в записи — 9, заголовок тоже содержит 9 полей.
	const nFields = 9
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "@")
		if len(f) != nFields {
			continue // повреждённая/неполная строка — пропускаем целиком
		}
		classify := trim(f[0])
		address := trim(f[1])
		name := trim(f[2])
		// Пропускаем строку заголовка и полностью пустые записи
		if classify == "Classify" || (classify == "" && address == "") {
			continue
		}
		params = append(params, map[string]string{
			"classify":    classify,
			"address":     address,
			"name":        name,
			"value":       trim(f[3]),
			"unit":        trim(f[4]),
			"dataType":    trim(f[5]),
			"notes":       trim(f[6]),
			"displayMode": trim(f[7]),
			"decimal":     trim(f[8]),
		})
	}
	return params
}

// API для страницы ASSUN: ?file=etalon.csv — полный список параметров,
// ?file=...&slave=N&regs=1000,100A,... — чтение значений из контроллера.
func (ws *WebServer) apiParameters(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	file := r.URL.Query().Get("file")
	if file == "" {
		file = "assun.csv"
	}
	if strings.ContainsAny(file, "/\\..") {
		http.Error(w, `{"error":"bad file name"}`, http.StatusBadRequest)
		return
	}
	content, err := dataFS.ReadFile("data/" + file)
	if err != nil {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}
	out := map[string]interface{}{
		"file":     file,
		"params":   parseAssunCSV(string(content)),
		"live":     false,
		"slave_id": 0,
	}

	// Опционально: чтение/запись holding-регистров устройства (Modbus RTU)
	q := r.URL.Query()
	if regsStr := q.Get("regs"); regsStr != "" {
		slaveID, e := strconv.ParseUint(q.Get("slave"), 10, 8)
		if e != nil || slaveID == 0 {
			out["error"] = "invalid slave id"
		} else {
			addrs := strings.Split(regsStr, ",")
			if len(addrs) > 125 {
				addrs = addrs[:125]
			}
			// Считываем блоки последовательных адресов
			values := make(map[string]int64, len(addrs))
			for k := 0; k < len(addrs); {
				base, e := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(addrs[k]), "0x"), 16, 16)
				if e != nil {
					k++
					continue
				}
				cnt := 1
				for k+cnt < len(addrs) && cnt < 125 {
					next, e := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(addrs[k+cnt]), "0x"), 16, 16)
					if e != nil || uint16(next) != uint16(base)+uint16(cnt) {
						break
					}
					cnt++
				}
				data, e := modbus_rtu.ReadHoldingRegisters(uint8(slaveID), uint16(base), uint16(cnt))
				if e != nil {
					out["error"] = e.Error()
					break
				}
				for j := 0; j < cnt; j++ {
					key := fmt.Sprintf("%04X", uint16(base)+uint16(j))
					v := int64(data[j*2])<<8 | int64(data[j*2+1])
					values[key] = v
				}
				k += cnt
			}
			out["live"] = true
			out["slave_id"] = slaveID
			out["values"] = values
		}
	}

	b, _ := json.Marshal(out)
	w.Write(b)
}

// POST /api/parameters — запись значения в holding-регистр.
// Тело запроса (JSON): {"slave":1,"address":"0x1001","value":"1.5","dataType":"Int16"}
func (ws *WebServer) apiWriteParameter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Slave    int    `json:"slave"`
		Address  string `json:"address"`
		Value    string `json:"value"`
		DataType string `json:"dataType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
		return
	}
	if req.Slave <= 0 || req.Slave > 247 {
		http.Error(w, `{"error":"invalid slave id"}`, http.StatusBadRequest)
		return
	}
	rawAddr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(req.Address)), "0x")
	address64, err := strconv.ParseUint(rawAddr, 16, 16)
	if err != nil {
		http.Error(w, `{"error":"invalid address"}`, http.StatusBadRequest)
		return
	}
	address := uint16(address64)

	val, err := strconv.ParseFloat(req.Value, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid value"}`, http.StatusBadRequest)
		return
	}

	is32 := strings.Contains(strings.ToLower(req.DataType), "32")
	writeErr := error(nil)
	if is32 {
		writeErr = modbus_rtu.WriteDoubleRegister(uint8(req.Slave), address, uint32(int64(val)))
	} else {
		writeErr = modbus_rtu.WriteSingleRegister(uint8(req.Slave), address, uint16(int64(val)))
	}
	if writeErr != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": writeErr.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "slave": req.Slave, "address": req.Address})
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
