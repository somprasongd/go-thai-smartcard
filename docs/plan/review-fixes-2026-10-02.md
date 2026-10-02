# ผลแก้ไข code review — 2026-10-02

แก้ทั้ง 21 findings บน branch `fix/review-findings` จาก `main` ที่ `a00a97e`.
ผลด้านล่างเป็นหลักฐานจากเครื่อง macOS arm64 เครื่องนี้ ยังไม่ได้ commit/push
หรือสร้าง release.

| # | Finding | การแก้และหลักฐาน |
| --- | --- | --- |
| 1 | Windows ขอ SCM/service ALL_ACCESS เกิน ACL ที่ installer ให้ | ขอ SC_MANAGER_CONNECT และสิทธิ์ query/start/stop เฉพาะ action; Windows ctl/tray cross-build ผ่าน |
| 2 | Live unit test สั่งหยุด service จริง | ลบคำสั่ง stop; live checks เหลืออ่าน status และใช้ build tag `integration`; opt-in checks ผ่าน |
| 3 | ภาพใน lightbox ค้างหลัง clear/remove | clearData ปิด lightbox และลบ src; Node ทดสอบ clear และ smc-removed |
| 4 | Trace มี permission 0644 | สร้าง directory 0700 และ atomic replacement 0600 รวมไฟล์เดิม; permission regression ผ่าน; trace ในเครื่องปรับเป็น private |
| 5 | Config/version มาจากคนละ snapshot | LoadVersion อ่าน bytes ครั้งเดียวแล้ว decode/hash; concurrent replacement regression ผ่าน; settings API ใช้ snapshot นี้ |
| 6 | Settings reader หายเมื่อ control queue เต็ม | ReaderStore แยก persisted selection จากคิวคำสั่ง; save พร้อมคิวเต็มและ daemon observation regression ผ่าน |
| 7 | Daemon retry กลับไปใช้ reader ตอน startup | ทุก retry อ่าน ReaderStore ปัจจุบัน; factory/retry regression ตรวจ first → second reader |
| 8 | PC/SC startup failure ทำให้ transport เป็น nil ตลอด | เปิด transport ใหม่ทุก retry และปิด context เดิมเมื่อ loop จบ; factory failure/recovery regression ผ่าน |
| 9 | Shutdown ปิด transport ขณะ read loop ยังใช้อยู่ | เจ้าของ transport รอ daemon จบและคืน session ก่อน close; broadcast ส่งแบบยกเลิกได้; cleanup-order regression และ SIGTERM → real capture ผ่าน |
| 10 | Tray polling/action เขียน MenuItem พร้อมกัน | ล็อก service UI refresh; concurrent menu regression ผ่าน race detector |
| 11 | macOS ไม่มี helper ทำให้ใช้ fallback ไม่ได้ | ถ้ามี installed plist คืน unknown ที่อนุญาต explicit actions; poll ไม่ถาม password; missing/installed/denied regression ผ่าน |
| 12 | Windows poll ไม่ปิด service handle | cleanup ปิดทั้ง service และ SCM handles รวม error paths; Windows cross-build ผ่าน |
| 13 | Retired socket.io Serve goroutine ค้าง / Close อาจ panic | Local v1.6.2 fork ใช้ shutdown signal แทน close connChan และติดตาม initializing/accepted sessions; repeat retirement, queued/unfinished handshakes และ concurrent handshake tests ผ่าน race detector |
| 14 | ID checksum ที่ควรเป็น 1 ถูกเปลี่ยนเป็น 0 | เอาการแปลง 1 → 0 ออก; Node ทดสอบ 100 synthetic valid IDs และ invalid counterparts |
| 15 | Boot ทับ privacy preferences ที่บันทึกแล้ว | ตั้ง hadStoredPrefs ก่อน loadPrefs; Node ทดสอบ persisted toggles และ first-visit defaults |
| 16 | Language migration ทับ dataLang รูปแบบปัจจุบัน | migrate เฉพาะเมื่อไม่มี dataLang; Node ทดสอบภาษา UI/data แยกกันและ legacy both |
| 17 | Refresh reader dropdown ทับตัวเลือกว่าง | ใช้ saved reader เฉพาะ initial load; refresh เก็บค่า dropdown รวมค่าว่าง; Node regression ผ่าน |
| 18 | Recorder connect ก่อนเสียบบัตร | WaitCardPresent แบบ bounded/cancellable ก่อน Connect; timeout/insertion/cancel regression และ real capture ผ่าน |
| 19 | Recorder ใช้ GET RESPONSE ผิด ATR | อ่าน Status และเลือกด้วย util.GetResponseCommand ให้ทุก applet; synthetic 3b67/legacy tests และ fresh strict replay ผ่าน |
| 20 | macOS sign คนละ app กับที่เข้า pkg | sign staged app และ agent ใน pkgroot พร้อม verify ก่อน pkgbuild; build/pack/expand แล้ว verify extracted ad-hoc signatures ผ่าน |
| 21 | Linux package ขาด PC/SC runtime dependencies | deb: libpcsclite1, pcscd; rpm: pcsc-lite-libs, pcsc-lite; สร้าง metadata fixtures ด้วย nfpm และตรวจ Depends/Requires ผ่าน |

## Local verification

ผ่านหลังแก้ไข:

- `make check` — build, unit tests ทุก package, vet, gofmt และ wasm card library.
- `go test -race ./pkg/server/ ./cmd/tray/ ./cmd/agent/ ./pkg/smc/`
- `go test -race ./pkg/transport/ ./pkg/config/ ./cmd/record/`
- `node pkg/server/web/settings_test.cjs`
- `node pkg/server/web/index_test.cjs`
- `GOOS=js GOARCH=wasm go build -o /tmp/smartcard-review-agent.wasm ./cmd/agent`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./pkg/ctl ./cmd/tray`
- `go test -tags integration ./cmd/agent -run 'TestDispatchControlWithRealKardianos|TestCtlDarwinWithoutHelper' -v`
- `git diff --check`

## Real card verification

PC/SC พบ Identive CLOUD 2700 R Smart Card Reader. Agent อ่านบัตรที่เสียบอยู่
และสั่ง `read-now` ซ้ำสำเร็จสองครั้งทาง WebSocket และอีกสองครั้งทาง socket.io.
ตรวจรูปแบบเลขบัตรและ checksum, ชื่อไทย/อังกฤษ, UTF-8 หลัง decode, ที่อยู่,
laser code และภาพ JPEG 148×178; response มี NHSO. ข้อมูลจากการอ่านซ้ำตรงกัน.
HTTP `/`, `/settings`, `/api/info`, `/api/readers` ตอบสำเร็จ.

ทดสอบ settings ด้วย config ชั่วคราว: response ย้ายพอร์ตสมบูรณ์, socket เดิม
ถูกปิด, listener ใหม่ใช้งานได้ และเลือกเครื่องอ่าน/ทุกเครื่องอ่านมีผลใน status.
Dev config เดิมไม่ได้ถูกแก้ระหว่างทดสอบนี้.

SIGTERM ปิด agent แล้ว recorder เปิด session บนบัตรเดิมได้ทันที. Capture ใหม่
มี 81 APDU exchanges และ replay แบบ strict ผ่านครบ พร้อม cleanup/permissions.
Trace อยู่ใน testdata ที่ Git ignore และเป็น 0600; directory เป็น 0700.
ไม่แสดงชื่อ ที่อยู่ เลขบัตร ภาพ หรือค่าข้อมูล NHSO ในรายงาน/log การตรวจ.

หลังตรวจกลับมารัน agent รุ่นที่แก้แล้วด้วย `config.dev.toml` ที่
`http://127.0.0.1:9900`.

## ขอบเขตหลักฐาน

Windows ตรวจได้ถึง cross-build และ source access/cleanup paths; ยังไม่ได้
ทดสอบกับ Windows SCM และ installed ACL จริง. Linux ตรวจ metadata packages
ด้วย fixture binary; ยังไม่ได้ติดตั้งหรือรันบน Linux (Docker daemon ไม่พร้อม).
macOS สร้างและแตก installer จริงเพื่อตรวจลายเซ็น ad-hoc ที่ staged/extracted;
ไม่ได้ใช้ Developer ID หรือทำ Apple notarization และไม่ได้ติดตั้ง package.

การแพตช์ socket.io มี provenance/license และรายละเอียดที่
`third_party/go-socket.io/PATCHES.md`; ต้องพิจารณาแพตช์นี้เมื่อเปลี่ยน upstream.
