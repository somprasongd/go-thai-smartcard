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

- Web Server Port -> SMC_AGENT_PORT=9898
- เปิด/ปิดการแสดงรูปถ่าย -> SMC_SHOW_IMAGE=true/false default=true
- เปิด/ปิดการแสดงข้อมูลสิทธิการรักษาจาก -> SMC_SHOW_NHSO=E=true/false default=false

## Client connect with socket.io

```javascript
<script>
  const socket = io.connect('http://localhost:9898');
  socket.on('connect', function () {

  });
  socket.on('smc-data', function (data) {
    console.log(data); // JSON {status: 200, description:, 'Success', data: {}
  });
  socket.on('smc-error', function (data) {
    console.log(data); // JSON {status: 500, description:, 'Error', data: {message: ''}
  });
  socket.on('smc-removed', function (data) {
    console.log(data); // JSON {status: 205, description:, 'Card Removed', data: {message: ''}
  });
  socket.on('smc-inserted', function (data) {
    console.log(data); // JSON {status: 202, description:, 'Card Inserted', data: {message: ''}
  });
</script>
```

## Other Version

- Nodejs Version: https://github.com/somprasongd/thai-smartcard-nodejs

## Donate

สนับสนุนได้ผ่านทาง Promptpay

<img src="https://bit.ly/3gusiz8">
