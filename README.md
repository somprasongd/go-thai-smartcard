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

### รันด้วย PM2

- Windows

```bash
npm install -g pm2 pm2-windows-startup
pm2-startup install
pm2 start .\bin\thai-smartcard-agent.exe --name smc
pm2 save
```

- Ubuntu

```bash
npm install -g pm2
pm2 start ./bin/thai-smartcard-agent --name smc
pm2 startup
pm2 save
```

- Mac

```bash
npm install -g pm2
pm2 start ./bin/thai-smartcard-agent --name smc
pm2 startup
pm2 save
```

## Example Client connect via socket.io

```javascript
<script>
  const socket = io.connect('http://localhost:9898');
  socket.on('connect', function () {

  });
  socket.on('smc-data', function (data) {
    console.log(data);
  });
  socket.on('smc-error', function (data) {
    console.log(data);
  });
  socket.on('smc-removed', function (data) {
    console.log(data);
  });
  socket.on('smc-inserted', function (data) {
    console.log(data);
  });
</script>
```

## Example Client connect via WebSokcet

```javascript
<script>
  // Connection to Websocker Server...
  if (window['WebSocket']) {
    conn = new WebSocket('ws://' + document.location.host + '/ws');
    console.log(document.location.host);
    conn.onopen = function (evt) {
      var item = document.getElementById('data-ws');
      item.innerHTML = '<b>Connected to WebSocket server</b>';
    };
    conn.onclose = function (evt) {
      var item = document.getElementById('data-ws');
      item.innerHTML = '<b>Disconnected to WebSocket server</b>';
    };
    conn.onmessage = function (evt) {
      var messages = evt.data.split('\n');
      console.log(messages);
      for (var i = 0; i < messages.length; i++) {
        var item = document.getElementById('data-ws');
        item.innerText = messages[i];
      }
    };
    conn.onerror = function (err) {
      console.log('Socket Error: ', err);
    };
  } else {
    var item = document.getElementById('data-ws');
    item.innerHTML = '<b>Your browser does not support WebSockets.</b>';
    appendLog(item);
  }
</script>
```

## Other Version

- Nodejs Version: https://github.com/somprasongd/thai-smartcard-nodejs

## Donate

สนับสนุนได้ผ่านทาง Promptpay

<img src="https://bit.ly/3gusiz8">
