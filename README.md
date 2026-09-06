# notif-layer

Cek layer Telegram terbaru setiap 30 menit. Jika naik, kirim notifikasi ke Telegram user `7506666666`.

Sumber layer: [`api.tl` Telegram Desktop](https://github.com/telegramdesktop/tdesktop/blob/dev/Telegram/SourceFiles/mtproto/scheme/api.tl)

## Env

Di Vercel (Settings → Environment Variables):

| Nama | Wajib | Keterangan |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | ya | Token bot dari [@BotFather](https://t.me/BotFather) |
| `TELEGRAM_CHAT_ID` | tidak | Default `7506666666` |

User `7506666666` harus sudah menekan **Start** di bot, kalau tidak `sendMessage` gagal.

## Vercel

1. Import repo ini ke Vercel.
2. Isi env di atas.
3. Deploy production.

`vercel.json` memanggil `/api/cron` setiap 30 menit (`*/30 * * * *`).

**Hobby Vercel hanya boleh cron 1x sehari.** Untuk 30 menit:
- pakai **Pro**, atau
- pakai cron eksternal (cron-job.org) yang GET:

```
https://PROJECT.vercel.app/api/cron
```

## Tes lokal

```powershell
copy .env.example .env
# isi TELEGRAM_BOT_TOKEN
go run ./cmd/local
```

`go run ./cmd/local` membaca `.env`, cek sekarang, lalu tetap hidup dan cek lagi setiap 30 menit. Hentikan dengan Ctrl+C.

Sekali cek lalu keluar:

```powershell
go run ./cmd/local --once
```

Di Vercel isi env lewat Settings, bukan file `.env`. Vercel memanggil `/api/cron` sekali per jadwal, bukan proses yang hidup terus.

Atau setelah `vercel dev`:

```powershell
curl http://localhost:3000/api/cron
```
