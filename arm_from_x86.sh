cd rockpi-scan
go mod tidy
GOOS=linux GOARCH=arm64 go build -o rockpi-scan .   # Rock Pi S — arm64
./rockpi-scan -config /etc/rockpi-scan/config.json

Как правильно запустить команду в PowerShell:Разбейте команду на отдельные строки или объедините их через точку с запятой:
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o motd main.go
