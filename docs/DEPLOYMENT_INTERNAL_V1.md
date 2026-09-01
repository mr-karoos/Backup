# راهنمای استقرار و عملیات نسخه داخلی ۱ (Internal V1 Deployment Guide)

این سند راهنمای جامع و مرجع عملیاتی استقرار پلتفرم مدیریت پشتیبان‌گیری (`Backup Platform`) بر روی سرور تک‌نودی اوبونتو با استفاده از Docker Compose برای نسخه **Internal V1** است.

کلیه اصول ثبت‌شده در [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md)، [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md) و [docs/DECISIONS.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DECISIONS.md) (به‌ویژه ADR-001, ADR-002, ADR-003, ADR-008, ADR-019, ADR-024, ADR-025, ADR-029) در این استقرار رعایت شده‌اند.

---

## ۱. معماری و پیش‌نیازهای استقرار (Architecture & Prerequisites)

### ۱.۱. مدل استقرار (Deployment Model)
- پلتفرم به صورت یک **Modular Monolith** تک‌پروسس (شامل سرور HTTP API، استخر کارگرهای پس‌زمینه Worker Pool، زمان‌بند Scheduler، و پاک‌سازی‌کننده Stale Run Reaper) درون یک کانتینر Go اجرا می‌شود.
- پایگاه داده **PostgreSQL 15+** در کانتینر مجزا در شبکه ایزوله داخلی داکر اجرا می‌شود.
- پورت دیتابیس (`5432`) به هیچ وجه روی هاست پابلیش نمی‌شود.
- پورت وب پلتفرم (`8080`) به صورت پیش‌فرض فقط روی لوکال‌هاست سرور (`127.0.0.1`) بایند می‌شود.

### ۱.۲. مشخصات پیشنهادی سخت‌افزار سرور
- **سیستم‌عامل:** Ubuntu 22.04 LTS (یا Debian 12) x86_64
- **پردازنده:** حداقل ۴ هسته (توصیه: ۸ هسته)
- **حافظه رم:** حداقل ۸ گیگابایت (توصیه: ۱۶ گیگابایت)
- **فضای ذخیره‌سازی:** حداقل ۵۰ گیگابایت برای پایگاه داده و فایل‌سیستم محلی آرتیفکت‌ها (`/srv/backup-platform`)

### ۱.۳. نرم‌افزارهای الزامی روی هاست
- `Docker Engine` نسخه 24.0 یا بالاتر
- پلاگین `Docker Compose v2` (`docker compose`)
- ابزارهای کمکی لینوکس: `bash`, `openssl`, `systemd`

---

## ۲. ملاحظات امنیتی شبکه و دسترسی در نسخه داخلی ۱ (Network & Security Isolation)

> [!WARNING]
> **عدم افشای پورت به اینترنت عمومی بدون TLS (ADR-024)**
> در نسخه Internal V1 هنوز گواهی عمومی SSL/TLS فعال نشده است. پلتفرم **نباید** مستقیماً روی IP عمومی سرور (`0.0.0.0:8080`) در معرض اینترنت باز قرار گیرد. هرگونه لاگین و ارسال رمز روی HTTP خام در اینترنت عمومی اکیداً ممنوع است.

### ۲.۱. دسترسی امن از طریق تونل SSH (SSH Tunneling)
مدیران سیستم برای اتصال به پنل و APIها از طریق تونل SSH متصل می‌شوند:
```bash
ssh -L 8080:127.0.0.1:8080 administrator@SERVER_IP
```
سپس از طریق مرورگر یا ابزارهای API روی سیستم محلی خود به آدرس زیر دسترسی خواهند داشت:
```text
http://127.0.0.1:8080/api/v1/health
```

### ۲.۲. دسترسی از طریق شبکه خصوصی یا VPN
در صورتی که سرور به یک شبکه خصوصی شرکتی یا VPN متصل باشد، متغیر `BACKUP_PLATFORM_BIND_IP` در `deploy/.env` می‌تواند به IP آن کارت شبکه خصوصی تنظیم شود:
```bash
BACKUP_PLATFORM_BIND_IP=10.8.0.1
```

---

## ۳. سیاست کوکی‌ها و محیط اجرا (`APP_ENV` vs `AUTH_COOKIE_SECURE`)

ساختار پیکربندی پلتفرم از قوانین سخت‌گیرانه‌ای پیروی می‌کند:
- در محیط‌های `APP_ENV=production` و `APP_ENV=staging`، مقدار `AUTH_COOKIE_SECURE` **باید** `true` باشد (ترافیک منحصراً HTTPS).
- برای نسخه داخلی Internal V1 که از طریق تونل SSH یا شبکه محلی بدون TLS کار می‌کند:
  - `APP_ENV=development`
  - `AUTH_COOKIE_SECURE=false`
- این پیکربندی استاندارد اجازه می‌دهد کوکی `HttpOnly` رفرش توکن بدون نیاز به پروتکل HTTPS روی لوکال‌هاست فعال بماند بدون اینکه اعتبارسنجی امنیتی کدهای Go تضعیف شود.
- پس از تأیید و راه‌اندازی پروکسی معکوس (Reverse Proxy) و TLS عمومی در فازهای بعدی، مقادیر به `production` و `true` تغییر خواهند کرد.

---

## ۴. مراحل گام‌به‌گام استقرار (Step-by-Step Deployment Procedure)

### مرحله ۱: کلون یا دریافت کد پروژه
کد پروژه را در دایرکتوری استاندارد استقرار (مثلاً `/opt/backup-platform`) قرار دهید:
```bash
sudo mkdir -p /opt/backup-platform
sudo chown -R $USER:$USER /opt/backup-platform
git clone https://github.com/mr-karoos/Backup.git /opt/backup-platform
cd /opt/backup-platform
```

### مرحله ۲: آماده‌سازی متغیرهای محیطی (`deploy/.env`)
فایل نمونه را کپی کرده و دسترسی آن را بلافاصله محدود کنید:
```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
```

### مرحله ۳: تولید کلیدهای رمزنگاری و کلمات عبور
کلیدهای زیر را با ابزار `openssl` تولید کرده و درون `deploy/.env` جای‌گذاری نمایید:

1. **کلید رمزگذاری Master Key (AES-256-GCM):**
   ```bash
   openssl rand -base64 32
   ```
   مقدار را در متغیر `ENCRYPTION_MASTER_KEY` قرار دهید.

2. **کلید امضای توکن‌های دسترسی JWT:**
   ```bash
   openssl rand -base64 48
   ```
   مقدار را در متغیر `JWT_SIGNING_KEY` قرار دهید.

3. **رمز عبور تصادفی پایگاه داده PostgreSQL:**
   ```bash
   openssl rand -hex 24
   ```
   مقدار را در `POSTGRES_PASSWORD` و همچنین درون رشته اتصال `DATABASE_URL` قرار دهید.

4. **رمز عبور کاربر اولیه Bootstrap System Admin (در صورت نیاز):**
   ```bash
   openssl rand -hex 20
   ```

نمونه تکمیل‌شده `deploy/.env`:
```ini
BACKUP_PLATFORM_BIND_IP=127.0.0.1
BACKUP_PLATFORM_PORT=8080
BACKUP_PLATFORM_STORAGE_PATH=/srv/backup-platform

POSTGRES_DB=backup_platform
POSTGRES_USER=backup_platform
POSTGRES_PASSWORD=generated_postgres_password_hex

DATABASE_URL=postgres://backup_platform:generated_postgres_password_hex@postgres:5432/backup_platform?sslmode=disable

APP_ENV=development
LOG_LEVEL=info
AUTH_COOKIE_SECURE=false

JWT_SIGNING_KEY=generated_jwt_secret_base64
ENCRYPTION_MASTER_KEY=generated_master_key_base64_32bytes
ENCRYPTION_MASTER_KEY_VERSION=1

BOOTSTRAP_ADMIN_EMAIL=admin@internal.zone
BOOTSTRAP_ADMIN_PASSWORD=generated_admin_bootstrap_password
```

### مرحله ۴: آماده‌سازی دایرکتوری‌های ذخیره‌سازی هاست
اسکریپت آماده‌سازی دایرکتوری‌ها را با دسترسی root اجرا کنید تا مالکیت کاربری غیرروت کانتینر (`10001:10001`) و مجوزهای اکید `0700` تنظیم شوند:
```bash
sudo ./deploy/scripts/prepare-host.sh /srv/backup-platform
```

### مرحله ۵: اعتبارسنجی کانفیگ و ساخت ایمیج داکر
```bash
# بررسی سینتکس فایل compose.yaml
docker compose --env-file deploy/.env config

# ساخت کانتینر اپلیکیشن باینری Go
docker compose --env-file deploy/.env build
```

### مرحله ۶: راه‌اندازی سرویس‌ها در پس‌زمینه
```bash
docker compose --env-file deploy/.env up -d
```

### مرحله ۷: بررسی وضعیت و لاگ‌ها
```bash
# بررسی وضعیت سلامت کانتینرها (باید هر دو healthy باشند)
docker compose --env-file deploy/.env ps

# بررسی لاگ‌های استارتاپ و مایگریشن خودکار دیتابیس
docker compose --env-file deploy/.env logs -f
```

### مرحله ۸: تست و اعتبارسنجی Healthcheck
```bash
curl -i http://127.0.0.1:8080/api/v1/health
```
پاسخ مورد انتظار:
```http
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8

{"status":"ok"}
```

---

## ۵. زمان‌بندی پشتیبان‌گیری دوره‌ای از متادیتای PostgreSQL (Metadata Backups)

برای حفاظت از پایگاه داده متادیتا (اطلاعات کاربران، منابع، کردانشال‌های رمزشده، تاریخچه و پلن‌ها):

### ۵.۱. تست اجرای دستی اسکریپت بکاپ
```bash
sudo /opt/backup-platform/deploy/scripts/backup-metadata.sh /opt/backup-platform
```
خروجی موفقیت‌آمیز باید فایلی با ساختار زیر در `/srv/backup-platform/metadata-backups/` ایجاد کند:
```bash
ls -la /srv/backup-platform/metadata-backups/
# -rw------- 1 root root backup-platform-metadata-YYYYMMDD-HHMMSS.dump
```

### ۵.۲. فعال‌سازی سرویس و تایمر Systemd
```bash
# کپی فایل‌های سرویس و تایمر به مسیر سیستم‌دی
sudo cp deploy/systemd/backup-platform-metadata-backup.service /etc/systemd/system/
sudo cp deploy/systemd/backup-platform-metadata-backup.timer /etc/systemd/system/

# ریلود و فعال‌سازی تایمر
sudo systemctl daemon-reload
sudo systemctl enable --now backup-platform-metadata-backup.timer

# بررسی وضعیت تایمر
sudo systemctl status backup-platform-metadata-backup.timer
sudo systemctl list-timers --all | grep backup-platform
```

---

## ۶. دستورالعمل بازیابی بحران متادیتا (Manual Disaster Recovery & Restore)

> [!CAUTION]
> **عملیات دستی و حساس بازیابی متادیتا**
> بازیابی متادیتا وضعیتی حساس و غیرقابل بازگشت است. این فرآیند هرگز نباید به صورت خودکار اجرا شود و حتماً مستلزم توقف کانتینر اپلیکیشن پیش از شروع است.

### مراحل بازیابی:

1. **توقف کانتینر برنامه برای جلوگیری از تغییر داده‌ها:**
   ```bash
   cd /opt/backup-platform
   docker compose --env-file deploy/.env stop app
   ```

2. **انتخاب فایل بکاپ متادیتا:**
   ```bash
   ls -lat /srv/backup-platform/metadata-backups/
   # فایل مورد نظر را انتخاب کنید، مثلا:
   # /srv/backup-platform/metadata-backups/backup-platform-metadata-20260901-033000.dump
   ```

3. **ایجاد یک نسخه بکاپ اضطراری از وضعیت فعلی پیش از Restore:**
   ```bash
   sudo ./deploy/scripts/backup-metadata.sh /opt/backup-platform
   ```

4. **اجرای دستور بازیابی `pg_restore` داخل کانتینر PostgreSQL:**
   ```bash
   # پاک‌سازی آبجکت‌ها و بازنشانی دیتابیس با pg_restore
   cat /srv/backup-platform/metadata-backups/backup-platform-metadata-YYYYMMDD-HHMMSS.dump | \
   docker compose --env-file deploy/.env exec -T postgres \
   pg_restore -U backup_platform -d backup_platform --clean --if-exists
   ```

5. **راه‌اندازی مجدد کانتینر اپلیکیشن:**
   ```bash
   docker compose --env-file deploy/.env start app
   ```

6. **بررسی لاگ‌های ریکاوری استارتاپ و سلامت سرویس:**
   ```bash
   docker compose --env-file deploy/.env logs -f app
   curl -i http://127.0.0.1:8080/api/v1/health
   ```

---

## ۷. عملیات و نگهداری روزمره (Operations & Maintenance)

### ۷.۱. روتاسیون لاگ‌های داکر
کانتینرهای داکر روی فرمت `json-file` با محدودیت `max-size: "10m"` و `max-file: "5"` تنظیم شده‌اند تا لاگ‌ها فضای دیسک هاست را اشغال نکنند.

### ۷.۲. راه‌اندازی مجدد سرویس‌ها
```bash
docker compose --env-file deploy/.env restart
```

### ۷.۳. خاموش‌سازی اصولی (Graceful Shutdown)
```bash
docker compose --env-file deploy/.env down
```
*(داده‌های دیتابیس در ولوم `backup_platform_postgres_data` و فایل‌های آرتیفکت در `/srv/backup-platform` به صورت کاملاً پایدار حفظ می‌شوند).*

### ۷.۴. بررسی پایداری پس از ریبوت سرور
کانتینرها با سیاست `restart: unless-stopped` پیکربندی شده‌اند و با بالا آمدن مجدد سیستم‌عامل سرور، کانتینر دیتابیس و کانتینر اپلیکیشن به طور خودکار شروع به کار خواهند کرد.

---

## ۸. چک‌لیست نهایی استقرار امن (Secure Deployment Checklist)

قبل از تحویل یا بهره‌برداری، موارد زیر را کنترل و تایید نمایید:

- [ ] فایل `deploy/.env` توسط Git ردیابی نمی‌شود (`.gitignore`).
- [ ] دسترسی فایل `deploy/.env` روی مد `0600` قرار دارد.
- [ ] کلید `JWT_SIGNING_KEY` یکتا و با آنتروپی بالا (حداقل ۳۲ کاراکتر) تنظیم شده است.
- [ ] کلید `ENCRYPTION_MASTER_KEY` یکتا و دقیقاً ۳۲ بایت (AES-256) تولید شده است.
- [ ] کلمه عبور `POSTGRES_PASSWORD` پیچیده و تصادفی است.
- [ ] پورت دیتابیس `5432` روی هاست باز/پابلیش نشده است.
- [ ] پروسس اپلیکیشن داخل کانتینر به صورت غیرروت (`UID 10001`) اجرا می‌شود.
- [ ] دسترسی دایرکتوری‌های ذخیره‌سازی `/srv/backup-platform` روی `0700` و مالکیت `10001:10001` است.
- [ ] دسترسی آرتیفکت‌های پشتیبان روی `0600` و پوشه‌ها `0700` است.
- [ ] پورت ۸۰۸۰ هاست منحصراً به لوکال‌هاست (`127.0.0.1`) یا شبکه خصوصی بایند شده است.
- [ ] هیچ لاگین خام HTTP روی اینترنت عمومی صورت نمی‌گیرد (دسترسی با SSH Tunnel/VPN).
- [ ] روتاسیون لاگ‌های داکر در `compose.yaml` فعال است.
- [ ] پایگاه داده PostgreSQL از ولوم ماندگار (`postgres_data`) استفاده می‌کند.
- [ ] اجرای دستی اسکریپت `backup-metadata.sh` با موفقیت تست شده است.
- [ ] تایمر سیستم‌دی `backup-platform-metadata-backup.timer` فعال و فعال‌سازی شده است.
- [ ] آزمون Healthcheck خروجی `{"status":"ok"}` با کد وضعیت ۲۰۰ برمی‌گرداند.
- [ ] تست ری‌استارت کانتینرها بدون مشکل انجام می‌شود.
- [ ] هیچ سکرت یا رمزی در خروجی `git diff` و لاگ‌های داکر وجود ندارد.
