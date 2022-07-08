# go-thai-smartcard

โปรแกรมอ่านบัตรประชาชน ด้วยภาษา Go

## การ Build

- ติดตั้ง [Go](https://go.dev/dl/)
- Clone git repo https://github.com/somprasongd/go-thai-smartcard
- รันคำสั่ง `go mod download`
- Build ด้วยคำสั่ง `go build -o bin/thai-smartcard-agent ./cmd/agent/main.go`
  > ถ้าเป็น Windows ใส่ .exe ด้วย go build -o bin/thai-smartcard-agent.exe ./cmd/agent/main.go

## การใช้งาน

สามารถรันโปรแกรมได้จาก binary file ที่ build ออกมาได้เลย

**แก้ไขค่าเริ่มต้นด้วย env**

- Web Server Port -> SMC_PORT=9898
- เปิด/ปิดการแสดงรูปถ่าย -> SMC_SHOW_IMAGE=true/false default=true
- เปิด/ปิดการแสดงข้อมูลสิทธิการรักษาจาก -> SMC_SHOW_NHSO=E=true/false default=false
