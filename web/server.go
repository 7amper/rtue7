package web

import (
	"crypto/subtle"

	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"

	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"rtue7/config"
	"rtue7/modbus_rtu"
	"rtue7/modbus_tcp"
	"runtime"
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
	http.HandleFunc("/api/readall", ws.apiReadAll)
	http.HandleFunc("/api/read", ws.apiReadRegister)
	http.HandleFunc("/api/write", ws.authMiddleware(ws.apiWriteParameter))
	http.HandleFunc("/api/regfile/save", ws.authMiddleware(ws.apiRegFileSave))
	http.HandleFunc("/api/regfile/load", ws.authMiddleware(ws.apiRegFileLoad))
	http.HandleFunc("/api/regfile/list", ws.authMiddleware(ws.apiRegFileList))

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

		// Данные формы возвращаются в шаблон, чтобы поля не сбрасывались после чтения/записи
		formData := map[string]interface{}{
			"slave_id": r.FormValue("slave_id"),
			"reg_type": regType,
			"address":  rawAddr,
			"value":    valueStr,
		}

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
			formData["result"] = result
			ws.tmpl.ExecuteTemplate(w, "registers.html", formData)
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
			formData["ok"] = 1
			ws.tmpl.ExecuteTemplate(w, "registers.html", formData)
			return
		}
	}
	ws.tmpl.ExecuteTemplate(w, "registers.html", map[string]interface{}{})
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

// assunFileExists проверяет имя файла по белому списку: разрешены только
// CSV-файлы, фактически присутствующие во встраиваемом каталоге data/.
// Прежняя проверка strings.ContainsAny(file, "/\\..") ошибочно отклоняла
// любые имена с точкой (например, "etalon.csv") — Read All получал 400
// «bad file name» и чтение не выполнялось.
func assunFileExists(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || filepath.Ext(name) != ".csv" {
		return false
	}
	_, err := fs.Stat(dataFS, "data/"+name)
	return err == nil
}

// readLiveRegisters читает holding-регистры устройства по Modbus RTU.
// Каждый элемент списка — "адрес@Data Type", например "0x1004@Uint32":
// размер (1 или 2 регистра) и интерпретация значения берутся из Data Type.
// Возвращает карту "адрес HEX (без 0x)" -> сырое int64 значение и первую
// ошибку соединения (чтение прерывается на ней, уже полученные значения
// сохраняются — частичное чтение лучше полного отказа).
func readLiveRegisters(slaveID uint8, regsStr string) (map[string]int64, error) {
	type regReq struct {
		addr uint16
		size int  // количество 16-битных регистров: 1 или 2
		sign bool // true для Int16/Int32 (дополнительный код)
	}
	items := strings.Split(regsStr, ",")
	reqs := make([]regReq, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		size, sign := 1, false
		if at := strings.IndexByte(item, '@'); at >= 0 {
			dt := strings.ToLower(strings.TrimSpace(item[at+1:]))
			item = item[:at]
			if strings.Contains(dt, "32") {
				size = 2
			}
			sign = strings.HasPrefix(dt, "int") // Int16 / Int32
		}
		a64, e := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(item), "0x"), 16, 16)
		if e != nil {
			continue
		}
		reqs = append(reqs, regReq{uint16(a64), size, sign})
	}
	if len(reqs) > 125 {
		reqs = reqs[:125]
	}
	values := make(map[string]int64, len(reqs))
	var readErr error
	// Считываем блоки последовательных адресов с учётом размера значения.
	for k := 0; k < len(reqs) && readErr == nil; {
		base := reqs[k].addr
		cnt := reqs[k].size
		for k+cnt < len(reqs) && cnt < 125 {
			if reqs[k+cnt].addr != base+uint16(cnt) {
				break
			}
			cnt += reqs[k+cnt].size
		}
		data, e := modbus_rtu.ReadHoldingRegisters(slaveID, base, uint16(cnt))
		if e != nil {
			readErr = e
			break
		}
		words := make([]uint16, cnt)
		for j := 0; j < cnt; j++ {
			words[j] = binary.BigEndian.Uint16(data[j*2 : j*2+2])
		}
		// Интерпретация прочитанных слов согласно Data Type:
		// Uint16 — беззнаковое 16-бит; Int16 — знаковое 16-бит;
		// Uint32/Int32 — два слова (low word first, как в WriteDoubleRegister),
		// Int32 — знаковое (дополнительный код), Uint32 — беззнаковое.
		for j := 0; j < cnt; j += reqs[k+j].size {
			key := fmt.Sprintf("%04X", base+uint16(j))
			var v int64
			if reqs[k+j].size == 2 {
				u := uint32(words[j+1])<<16 | uint32(words[j])
				if reqs[k+j].sign {
					v = int64(int32(u)) // Int32
				} else {
					v = int64(u) // Uint32
				}
			} else if reqs[k+j].sign {
				v = int64(int16(words[j])) // Int16
			} else {
				v = int64(words[j]) // Uint16
			}
			values[key] = v
		}
		k += cnt
	}
	return values, readErr
}

// API для страницы ASSUN: ?file=etalon.csv — полный список параметров,
// ?file=...&slave=N&regs=1000,100A,... — чтение значений из контроллера.
func (ws *WebServer) apiParameters(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	q := r.URL.Query()
	file := q.Get("file")
	if file == "" {
		file = "assun.csv"
	}
	// Разрешены только известные CSV-файлы из встраиваемого каталога data/.
	// Проверка по белому списку (а не по подстроке ".."), чтобы корректно
	// работать с именами вида "etalon.csv".
	if !assunFileExists(file) {
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

	// Опционально: чтение holding-регистров устройства (Modbus RTU)
	if regsStr := q.Get("regs"); regsStr != "" {
		slaveID, e := strconv.ParseUint(q.Get("slave"), 10, 8)
		if e != nil || slaveID == 0 {
			out["error"] = "invalid slave id"
		} else {
			values, readErr := readLiveRegisters(uint8(slaveID), regsStr)
			out["live"] = true
			out["slave_id"] = slaveID
			// Прочитанные значения отправляем клиенту (ключ — адрес HEX без 0x),
			// чтобы кнопка Read All вписывала их в колонку Value.
			out["values"] = values
			if readErr != nil {
				out["error"] = readErr.Error()
			}
		}
	}

	b, _ := json.Marshal(out)
	w.Write(b)
}

// GET /api/readall?slave=N&regs=1000@Uint16,1001@Int16,... — массовое
// чтение holding-регистров напрямую с устройства по Modbus RTU (кнопка
// Read All на вкладке ASSUN). В отличие от /api/parameters здесь нет CSV:
// сервер возвращает только живые значения контроллера для выбранного Slave ID.
func (ws *WebServer) apiReadAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	q := r.URL.Query()
	out := map[string]interface{}{"live": false, "slave_id": 0, "values": map[string]int64{}}
	slaveID, e := strconv.ParseUint(q.Get("slave"), 10, 8)
	if e != nil || slaveID == 0 {
		out["error"] = "invalid slave id"
	} else if regsStr := q.Get("regs"); regsStr == "" {
		out["error"] = "no registers requested"
	} else {
		values, readErr := readLiveRegisters(uint8(slaveID), regsStr)
		out["live"] = true
		out["slave_id"] = slaveID
		out["values"] = values
		if readErr != nil {
			out["error"] = readErr.Error()
		}
	}
	b, _ := json.Marshal(out)
	w.Write(b)
}

// GET /api/read?slave=N&address=0x1002&type=Int32 — чтение одного значения.
// Тип берётся из колонки Data Type: Uint16/Int16 — 1 регистр, Uint32/Int32 — 2 регистра;
// Int16/Int32 интерпретируются как знаковые (дополнительный код),
// Uint16/Uint32 — как беззнаковые.
func (ws *WebServer) apiReadRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	q := r.URL.Query()
	slaveID, e := strconv.ParseUint(q.Get("slave"), 10, 8)
	if e != nil || slaveID == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid slave id"})
		return
	}
	rawAddr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(q.Get("address"))), "0x")
	addr64, e := strconv.ParseUint(rawAddr, 16, 16)
	if e != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid address"})
		return
	}
	dt := strings.ToLower(strings.TrimSpace(q.Get("type")))
	count := 1
	if strings.Contains(dt, "32") {
		count = 2
	}
	sign := strings.HasPrefix(dt, "int") // Int16 / Int32
	data, e := modbus_rtu.ReadHoldingRegisters(uint8(slaveID), uint16(addr64), uint16(count))
	if e != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": e.Error()})
		return
	}
	var value int64
	if count == 2 {
		// low word first — тот же порядок слов, что в WriteDoubleRegister
		lo := binary.BigEndian.Uint16(data[0:2])
		hi := binary.BigEndian.Uint16(data[2:4])
		u := uint32(hi)<<16 | uint32(lo)
		if sign {
			value = int64(int32(u)) // Int32
		} else {
			value = int64(u) // Uint32
		}
	} else {
		w16 := binary.BigEndian.Uint16(data[0:2])
		if sign {
			value = int64(int16(w16)) // Int16
		} else {
			value = int64(w16) // Uint16
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"slave":   slaveID,
		"address": strings.ToUpper(rawAddr),
		"value":   value,
	})
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

	// Значение — уже целое (raw), масштабирование по Decimal place выполняет фронтенд.
	val, err := strconv.ParseFloat(req.Value, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid value"}`, http.StatusBadRequest)
		return
	}

	is32 := strings.Contains(strings.ToLower(req.DataType), "32")
	writeErr := error(nil)
	if is32 {
		// Uint32/Int32: приводим к 32-битному беззнаковому шаблону
		// (отрицательные Int32 — дополнительный код).
		writeErr = modbus_rtu.WriteDoubleRegister(uint8(req.Slave), address, uint32(uint64(int64(val))))
	} else {
		// Uint16/Int16: младшие 16 бит шаблона значения.
		writeErr = modbus_rtu.WriteSingleRegister(uint8(req.Slave), address, uint16(int64(val)))
	}
	if writeErr != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": writeErr.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "slave": req.Slave, "address": req.Address})
}

// ============================================================================
// Load/Save значений регистров в файл (кнопки Load и Save на странице ASSUN).
//
// Каталог файлов зависит от ОС (как в main.go для asot.json):
//   - Windows: рядом с исполняемым файлом (каталог запуска);
//   - Linux:   /etc/asot (запись требует прав root — как у systemd-сервиса
//     asot.service; при отказе записи клиенту возвращается ошибка доступа).
// Имя файла пользователь вводит в диалоге на странице; сервер проверяет его
// (без путей и «..», расширение .json добавляется автоматически).
// ============================================================================

const regFileDirLinux = "/etc/asot"

// regFilesDir возвращает каталог для файлов пресетов регистров.
func regFilesDir() string {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			return filepath.Dir(exe)
		}
		return "."
	}
	return regFileDirLinux
}

// regFilePath разрешает имя файла в каталоге пресетов; пустое имя или имя
// с путевыми разделителями — ошибка (защита от path traversal).
func regFilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/"\`) || name == "." || name == ".." {
		return "", fmt.Errorf("bad file name")
	}
	if !strings.EqualFold(filepath.Ext(name), ".json") {
		name += ".json"
	}
	return filepath.Join(regFilesDir(), name), nil
}

// regFileEntry — один сохранённый регистр: адрес HEX, сырое (масштабированное
// по Decimal place) значение и Data Type для корректной интерпретации.
type regFileEntry struct {
	Address  string `json:"address"`
	Value    int64  `json:"value"`
	DataType string `json:"dataType,omitempty"`
}

// regFileData — содержимое файла пресета.
type regFileData struct {
	Slave     int            `json:"slave,omitempty"`
	Tab       string         `json:"tab,omitempty"`
	Source    string         `json:"source,omitempty"`
	SavedAt   string         `json:"savedAt,omitempty"`
	Registers []regFileEntry `json:"registers"`
}

// GET /api/regfile/list — имена доступных файлов пресетов (подсказки на странице).
func (ws *WebServer) apiRegFileList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	names := []string{}
	out := map[string]interface{}{"dir": regFilesDir(), "files": names}
	entries, err := os.ReadDir(regFilesDir())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
				continue
			}
			names = append(names, e.Name())
		}
	}
	out["files"] = names
	b, _ := json.Marshal(out)
	w.Write(b)
}

// POST /api/regfile/save — сохранить значения регистров в файл.
// Тело запроса (JSON): {"name":"preset1","slave":1,"tab":"basic",
//
// "source":"assun.csv","registers":[{"address":"0x1000","value":10,"dataType":"Uint16"}]}
func (ws *WebServer) apiRegFileSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name      string         `json:"name"`
		Slave     int            `json:"slave"`
		Tab       string         `json:"tab"`
		Source    string         `json:"source"`
		SavedAt   string         `json:"savedAt"`
		Registers []regFileEntry `json:"registers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
		return
	}
	path, err := regFilePath(req.Name)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	if len(req.Registers) == 0 {
		http.Error(w, `{"error":"no registers to save"}`, http.StatusBadRequest)
		return
	}
	data := regFileData{Slave: req.Slave, Tab: req.Tab, Source: req.Source,
		SavedAt: req.SavedAt, Registers: req.Registers}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, `{"error":"marshal failed"}`, http.StatusInternalServerError)
		return
	}
	if runtime.GOOS != "windows" {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			log.Printf("regfile mkdir %s: %v", filepath.Dir(path), mkErr) // попробуем записать и так
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		http.Error(w, `{"error":"write failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": path, "count": len(req.Registers)})
}

// POST /api/regfile/load — прочитать файл пресета и вернуть регистры.
// Тело запроса (JSON): {"name":"preset1"}
func (ws *WebServer) apiRegFileLoad(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
		return
	}
	path, err := regFilePath(req.Name)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"error":"read failed: `+err.Error()+`"}`, http.StatusNotFound)
		return
	}
	var data regFileData
	if err := json.Unmarshal(content, &data); err != nil {
		http.Error(w, `{"error":"bad file format"}`, http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"path":      path,
		"slave":     data.Slave,
		"tab":       data.Tab,
		"source":    data.Source,
		"savedAt":   data.SavedAt,
		"registers": data.Registers,
	})
}
