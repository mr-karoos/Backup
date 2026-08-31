# طراحی رابط برنامه‌نویسی کاربردی (API Design)

این سند استانداردها، ساختار Endpointها، الگوهای احراز هویت و مجوزدهی، قالب پاسخ‌ها و ضوابط امنیتی `REST API` برای بک‌اند پلتفرم مدیریت پشتیبان‌گیری را بر اساس اسناد [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md)، [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md) و [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md) تعریف می‌کند.

---

## ۱. قواعد و اصول کلی طراحی API (General API Principles)

* **پروتکل ارتباطی و استاندارد**: تمامی ارتباطات بر بستر پروتکل امن **HTTPS** و مطابق با اصول معماری **RESTful** به صورت Stateless با تبادل داده‌های `JSON` انجام می‌پذیرد.
* **نسخه‌گذاری از ابتدا (API Versioning)**: تمام مسیرهای API با پیشوند نسخه مشخص شروع می‌شوند:
  ```text
  Base Path: /api/v1
  ```
* **آگاهی سازمانی و ایزولاسیون کامل (Organization-Aware Context)**:
  * به دلیل ساختار چندسازمانی (Multi-tenancy)، کلاینت‌ها پس از احراز هویت، سازمان فعال هدف را از طریق هدر استاندارد `X-Organization-ID` (حاوی UUID سازمان) ارسال می‌کنند یا سازمان فعال پیش‌فرض در توکن سشن/احراز هویت قرار می‌گیرد.
  * Middlewareهای سیستم پیش از اجرای کنترلر، عضویت و نقش کاربر در سازمان ارسالی را اعتبارسنجی کرده و زمینه (`Tenant Context`) را به درخواست تزریق می‌کنند.
* **منع قطعی ارجاع و نشت متقاطع بین‌سازمانی (Strict Tenant Isolation)**: هیچ کاربری نمی‌تواند به موجودیت‌های خارج از سازمان فعال خود دسترسی پیدا کند؛ در صورت عدم تطابق شناسه سازمان با موجودیت درخواستی، پاسخ `404 Not Found` (یا `403 Forbidden`) بازگردانده می‌شود.
* **محرمانگی کامل سکرت‌ها (Secret & Credential Non-Disclosure)**: فیلدهای حساس نظیر پسوردها، کلیدهای خصوصی SSH، توکن‌های cPanel و مقادیر رمزشده `encrypted_secret` **هرگز** در هیچ خروجی API بازگردانده نخواهند شد (فقط متادیتای عمومی نظیر `fingerprint`، نوع و نام کردانشال نمایش داده می‌شود).

---

## ۲. بخش اول: احراز هویت (Authentication API)

### ۱. ورود به سیستم (`POST /api/v1/auth/login`)

* **هدف**: اعتبارسنجی هویت کاربر بر اساس ایمیل و کلمه عبور، ایجاد سشن امن و صدور توکن‌های دسترسی.
* **نرخ درخواست (Rate Limit)**: اعمال محدودیت چندلایه هوشمند:
  * محدودیت بر اساس **آدرس IP**: حداکثر ۵ تلاش در هر دقیقه.
  * محدودیت ترکیبی بر اساس **(IP, Email/Identifier)** و ایمیل هدف جهت پیشگیری از حملات توزیع‌شده Credential Stuffing و حملات Brute-force متمرکز.

#### ساختار درخواست (Request):
```http
POST /api/v1/auth/login HTTP/1.1
Host: api.backup-platform.local
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "SuperSecretPassword123!"
}
```

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "user": {
      "id": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
      "email": "admin@example.com",
      "full_name": "System Administrator",
      "is_system_admin": true,
      "created_at": "2026-08-20T10:00:00Z"
    },
    "tokens": {
      "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
      "token_type": "Bearer",
      "expires_in": 900
    },
    "default_organization_id": "b1eebc99-9c0b-4ef8-bb6d-6bb9bd380b22"
  },
  "message": "ورود با موفقیت انجام شد.",
  "request_id": "req-9f2c8d1a-4e3b-4112-9c31-7e8a9d123456"
}
```

* **استراتژی توکن و مدیریت سشن (Token & Session Strategy)**:
  * **Access Token**: توکن سبک کوتاه‌مدت (Short-lived JWT، انقضا: ۱۵ دقیقه) شامل کلیم‌های `user_id`، `session_id` و `is_system_admin`، امضاشده با کلید امن سرور و ارسال از طریق هدر استاندارد `Authorization: Bearer <token>`.
  * **Refresh Token / Session Management**: توکن تصادفی با آنتروپی بالا (Opaque Random Token، انقضا: ۷ روز) نگهداری‌شده در کوکی امن با فلگ‌های `HttpOnly`، `Secure` (در زمان فعال بودن HTTPS)، و `SameSite=Strict` جهت تمدید سشن و جلوگیری از حملات XSS/CSRF. توکن خام صرفاً سمت کلاینت ذخیره شده و در دیتابیس تنها هش امن آن در جدول `user_sessions` نگهداری می‌شود.
  * **مدیریت سمت سرور و ابطال بلادرنگ (Server-Side Session & Immediate Revocation)**: در هر درخواست احرازهویت‌شده، علاوه بر اعتبارسنجی امضای JWT، وضعیت فعال بودن `session_id` در جدول `user_sessions` بررسی می‌شود. در صورت خروج کاربر (`Logout`)، تغییر کلمه عبور یا مسدودسازی حساب، نشست متناظر در دیتابیس با مقداردهی `revoked_at` فوراً ابطال می‌گردد و توکن‌های قدیمی نامعتبر می‌شوند.
  * با هر ورود موفق یا ناموفق، رویدادهای `auth.login.success` یا `auth.login.failed` با ثبت IP در Audit Log درج می‌گردند.

#### ساختار پاسخ ناموفق (`401 Unauthorized`):
```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "ایمیل یا کلمه عبور واردشده صحیح نمی‌باشد.",
    "details": null
  },
  "request_id": "req-9f2c8d1a-4e3b-4112-9c31-7e8a9d123457"
}
```

---

### ۲. تمدید توکن دسترسی (`POST /api/v1/auth/refresh`)

* **هدف**: تمدید Access Token منقضی‌شده با استفاده از Refresh Token ذخیره‌شده در کوکی امن، چرخش (Rotate) توکن رفرش و صدور توکن‌های جدید.
* **احراز هویت**: توکن رفرش از کوکی امن `HttpOnly` استخراج شده و پس از محاسبه هش با `user_sessions.refresh_token_hash` تطبیق داده می‌شود.

#### ساختار درخواست (Request):
```http
POST /api/v1/auth/refresh HTTP/1.1
Host: api.backup-platform.local
Cookie: refresh_token=r_9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d...
```

* **فرآیند اعتبارسنجی و چرخش (Validation & Rotation)**:
  1. مقایسه هش توکن دریافتی با `refresh_token_hash` در جدول `user_sessions`.
  2. بررسی فعال بودن، عدم انقضا (`expires_at > NOW()`) و عدم ابطال (`revoked_at IS NULL`) نشست و فعال بودن حساب کاربری.
  3. تولید توکن رفرش تصادفی جدید، محاسبه هش و به‌روزرسانی رکورد `user_sessions` (همراه با به‌روزرسانی `last_used_at`).
  4. صدور Access Token جدید حاوی `session_id`.
  5. تنظیم کوکی امن جدید `HttpOnly` با توکن رفرش جدید (توکن خام رفرش هرگز در بدنه JSON یا لاگ‌ها بازگردانده نمی‌شود).

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "tokens": {
      "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
      "token_type": "Bearer",
      "expires_in": 900
    }
  },
  "message": "توکن دسترسی با موفقیت تمدید شد.",
  "request_id": "req-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
}
```

---

### ۳. خروج از سیستم (`POST /api/v1/auth/logout`)

* **هدف**: ابطال سشن کاربر (`user_sessions.revoked_at = NOW()`)، منقضی‌کردن توکن بازنشانی و پاک‌سازی کوکی‌های احراز هویت.
* **ثبت حسابرسی**: ثبت رخداد `auth.logout` در جدول لاگ‌های حسابرسی.

#### ساختار درخواست (Request):
```http
POST /api/v1/auth/logout HTTP/1.1
Host: api.backup-platform.local
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": null,
  "message": "خروج از سیستم با موفقیت انجام شد.",
  "request_id": "req-8e1c7b0a-3d2a-4001-8b20-6d7a8c012345"
}
```

---

### ۴. دریافت مشخصات کاربر جاری (`GET /api/v1/auth/me`)

* **هدف**: بازیابی پروفایل کاربر لاگین‌شده، فهرست سازمان‌هایی که کاربر عضو آن‌هاست، نقش کاربر در هر سازمان و دسترسی‌های پایه.

#### ساختار درخواست (Request):
```http
GET /api/v1/auth/me HTTP/1.1
Host: api.backup-platform.local
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "user": {
      "id": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
      "email": "admin@example.com",
      "full_name": "System Administrator",
      "is_system_admin": true,
      "status": "active"
    },
    "memberships": [
      {
        "organization_id": "b1eebc99-9c0b-4ef8-bb6d-6bb9bd380b22",
        "organization_name": "Internal Organization",
        "organization_slug": "internal-org",
        "is_default_internal": true,
        "role": "admin",
        "status": "active",
        "permissions": [
          "resource:read",
          "resource:write",
          "credential:write",
          "backup_plan:read",
          "backup_plan:write",
          "backup_job:execute",
          "backup_artifact:download",
          "backup_artifact:delete",
          "audit_log:read"
        ]
      }
    ]
  },
  "message": "اطلاعات کاربر با موفقیت دریافت شد.",
  "request_id": "req-7d0b6a9f-2c1f-3990-7a19-5c6b7b901234"
}
```

---

## ۳. بخش دوم: طراحی مجوزدهی و سطوح دسترسی (Authorization Design)

مدل مجوزدهی پلتفرم در ۳ لایه اعتبارسنجی متوالی پیاده‌سازی می‌شود:

```text
[HTTP Request]
       │
       ▼
1. [User Authentication Middleware] ─── (بررسی امضا و انقضای JWT یا Session)
       │
       ▼
2. [Organization Context Middleware] ── (بررسی عضویت کاربر در Organization فعال و تزریق Context)
       │
       ▼
3. [Role & Permission Guard] ────────── (بررسی نقش کاربر و مجوزهای لازم برای Endpoint)
       │
       ▼
[Controller / Service Execution]
```

### نقش‌های اولیه در نسخه ۱ (Initial Roles in V1):

1. **`admin` (مدیر سازمان)**: دسترسی کامل به کلیه قابلیت‌ها و منابع درون سازمان (شامل تعریف منابع، اتصال‌دهنده‌ها، کردانشال‌ها، پلن‌ها، اجرای دستی، دانلود و حذف بکاپ‌ها و مشاهده Audit Logها).
2. **`member` (کارشناس عملیات)**: امکان مشاهده منابع و وضعیت جاب‌ها، اجرای دستی بکاپ‌های از پیش تعریف‌شده و دانلود فایل‌های بکاپ؛ **فاقد** دسترسی برای ویرایش کردانشال‌ها یا حذف فایل‌های بکاپ و منابع.
3. **`viewer` (ناظر / حسابرس)**: دسترسی صرفاً خواندنی (Read-Only) برای مانیتورینگ وضعیت سیستم، لاگ‌های جاب‌ها و گزارشات، بدون امکان تغییر تنظیمات، اجرا، دانلود یا حذف.

### ماتریس مجوزهای عملیات حساس (Permissions Matrix):

| عملیات / منبع هدف | Endpoint مرتبط | `admin` | `member` | `viewer` | توضیحات امنیتی |
| :--- | :--- | :---: | :---: | :---: | :--- |
| **ایجاد سازمان جدید (Platform Level)** | `POST /api/v1/organizations` | ❌* | ❌ | ❌ | *منحصراً نیازمند دسترسی سوپرادمین کل پلتفرم (`is_system_admin = true`). |
| **ایجاد / ویرایش / آرشیو Resource** | `POST/PUT/DELETE /api/v1/resources` | ✅ | ❌ | ❌ | نیاز به پیکربندی سرور هدف یا تغییر وضعیت به Archived. |
| **تغییر / ثبت / حذف Credential** | `POST/PUT/DELETE /api/v1/credentials` | ✅ | ❌ | ❌ | دسترسی انحصاری جهت جلوگیری از تغییر یا سرقت سکرت‌ها. |
| **تست اتصال به منبع** | `POST /api/v1/resources/{id}/test-connection` | ✅ | ❌ | ❌ | در نسخه ۱ پیش‌فرض ادمین است (آماده برای مجوز مستقل `resource:test`). |
| **شناسایی خودکار دیتابیس‌ها** | `GET /api/v1/resources/{id}/databases` | ✅ | ❌ | ❌ | کنترل‌شده با پرمیشن و محدود به ادمین در V1 (`resource:discover`). |
| **ایجاد جاب بکاپ (Create Job)** | `POST /api/v1/backup-jobs` | ✅ | ✅* | ❌ | ثبت در وضعیت `pending`. *نقش Member صرفاً مجاز به اجرای Plan از پیش تاییدشده است. |
| **اعتبارسنجی سلامت بکاپ (Verify)** | `POST /api/v1/backup-runs/{id}/verify` | ✅ | ✅ | ❌ | بررسی یکپارچگی Checksum و آرشیو بدون بازیابی کامل. |
| **دانلود آرتیفکت بکاپ** | `GET /api/v1/backup-artifacts/{id}/download` | ✅ | ✅ | ❌ | ثبت اجباری رخداد `backup.download` در Audit Log. |
| **حذف فیزیکی آرتیفکت بکاپ** | `DELETE /api/v1/backup-artifacts/{id}` | ✅ | ❌ | ❌ | عملیات مخرب و نیازمند نقش ادمین با لاگ حسابرسی. |
| **مدیریت Planهای بکاپ** | `POST/PUT/DELETE /api/v1/backup-plans` | ✅ | ❌ | ❌ | تغییر سیاست زمان‌بندی و نگهداری. |
| **مشاهده Audit Logها** | `GET /api/v1/audit-logs` | ✅ | ❌ | ❌ | بررسی وقایع امنیتی و تغییرات حساس. |

---

## ۴. بخش سوم: استاندارد پاسخ‌های API (Response Standard)

تمامی پاسخ‌های API از یک قالب یکنواخت و قابل پیش‌بینی با فیلدهای استاندارد پیروی می‌کنند.

### ۱. ساختار پاسخ‌های موفق (Success Response Format):
```json
{
  "data": {},
  "message": "عملیات با موفقیت انجام شد.",
  "request_id": "req-9f2c8d1a-4e3b-4112-9c31-7e8a9d123456"
}
```

* `data` (`object` یا `array` یا `null`): محتوای اصلی نتیجه درخواست.
* `message` (`string`): پیام کاربرپسند و قابل نمایش در رابط کاربری.
* `request_id` (`string`): شناسه یکتای ردیابی درخواست جهت پشتیبانی و پیگیری در لاگ‌های سیستمی.

### ۲. ساختار پاسخ‌های خطا (Error Response Format):
```json
{
  "error": {
    "code": "RESOURCE_NOT_FOUND",
    "message": "منبع درخواستی در این سازمان یافت نشد.",
    "details": null
  },
  "request_id": "req-9f2c8d1a-4e3b-4112-9c31-7e8a9d123456"
}
```

* `error.code` (`string`): کد خطای استاندارد و ثابت جهت مدیریت برنامه‌نویسی‌شده در فرانت‌اند (Machine-readable).
* `error.message` (`string`): پیام خطای توصیفی و پاک‌سازی‌شده برای کاربر (Human-readable).
* `error.details` (`any`): جزئیات ساختاریافته خطا (نظیر لیست خطاهای اعتبارسنجی فیلدها - قابل تهی).

### ۳. کدهای وضعیت HTTP و معادل خطاهای استاندارد (Standard HTTP Status Codes):

| کد وضعیت | کد خطای پیشنهادی (`code`) | شرح و کاربرد |
| :--- | :--- | :--- |
| `200 OK` | - | درخواست با موفقیت پردازش شد. |
| `201 Created` | - | منبع جدید با موفقیت ایجاد گردید. |
| `202 Accepted` | - | درخواست پذیرفته شده و به صف پایدار PostgreSQL تحویل گردیده است (به معنی آغاز بلادرنگ یا موفقیت پردازش ناهمگام نیست). |
| `204 No Content` | - | عملیات موفقیت‌آمیز بدون بدنه پاسخ (مثال: حذف). |
| `400 Bad Request` | `BAD_REQUEST` | ساختار JSON نامعتبر یا پارامترهای ناقص. |
| `401 Unauthorized` | `UNAUTHORIZED` / `INVALID_CREDENTIALS` | کاربر احراز هویت نشده، توکن منقضی یا نامعتبر است. |
| `403 Forbidden` | `FORBIDDEN` / `INSUFFICIENT_PERMISSIONS` | دسترسی به منبع یا اجرای عملیات برای نقش کاربر مجاز نیست. |
| `404 Not Found` | `NOT_FOUND` / `RESOURCE_NOT_FOUND` | موجودیت درخواستی در این سازمان وجود ندارد. |
| `409 Conflict` | `CONFLICT` / `ALREADY_EXISTS` | تداخل منطقی (مانند نام تکراری منبع یا جاب هم‌زمان در حال اجرا). |
| `422 Unprocessable` | `VALIDATION_FAILED` | داده‌های ورودی از نظر ساختار معتبرند اما قوانین اعتبارسنجی را نقض کرده‌اند (همراه با جزئیات فیلدها در `details`). |
| `429 Too Many Req` | `RATE_LIMIT_EXCEEDED` | تعداد درخواست‌ها از حد مجاز فراتر رفته است. |
| `500 Internal Error` | `INTERNAL_SERVER_ERROR` | خطای غیرمنتظره سرور (همراه با پیام پاک‌سازی‌شده عمومی بدون افشای Stack Trace). |

---

## ۵. بخش چهارم: ضوابط و الزامات امنیتی API (Security Rules)

1. **محدودسازی نرخ درخواست (Rate Limiting)**:
   * مسیر `/api/v1/auth/login`: اعمال محدودیت چندبعدی ترکیبی بر اساس **IP** و **(IP, Email/Identifier)** جهت مهار هم‌زمان حملات Brute-force و حملات توزیع‌شده Credential Stuffing.
   * سایر مسیرهای API: حداکثر ۱۰۰ درخواست در دقیقه به ازای هر کاربر/سازمان جهت حفاظت در برابر رفتارهای غیرعادی و DoS.
2. **پیشگیری از افشای داده‌های حساس (Zero Plaintext Credential Exposure)**:
   * فیلدهای پسورد، توکن، و کلیدهای خصوصی هرگز در بدنه پاسخ‌های GET، POST، PUT بازگردانده نمی‌شوند.
   * پاسخ‌های مدل Credential صرفاً شامل `id`, `name`, `type`, `fingerprint`, `created_at` هستند.
3. **پاک‌سازی پاسخ‌های خطا (Sanitized Error Responses)**:
   * سیستم هرگز خطاهای خام دیتابیس، خطاهای درایور SSH/cPanel، یا ساختار داخلی فایل‌سیستم سرور را به کاربر خروجی نمی‌دهد.
   * کلیه خطاهای سیستمی قبل از ارسال به کلاینت به پیام‌های کلی و امن ترجمه می‌شوند و جزئیات فنی فقط در لاگ‌های سرور همراه با `request_id` ثبت می‌گردد.
4. **رویدادهای ثبت حسابرسی (Audit Events)**:
   * کلیه عملیات حساس به صورت غیرهمگام (Asynchronous) اما پایدار در جدول `audit_logs` ثبت می‌شوند:
     * `user.login.success` / `user.login.failed`: هنگام احراز هویت کاربران.
     * `credential.access` / `credential.create` / `credential.update`: صرفاً برای مشاهده، فراخوانی یا ویرایش سکرت‌ها توسط کاربر/ادمین از طریق API؛ استفاده داخلی و خودکار کارگرهای پس‌زمینه (Worker) از Credentialها در حین اجرای بکاپ، رویداد Audit Log تولید **نمی‌کند** تا از اتلاف منابع و پر شدن بی‌مورد لاگ‌ها جلوگیری شود.
     * `resource.created` / `resource.updated` / `resource.deleted`: در زمان تغییر منابع سروری.
     * `backup.download`: در زمان درخواست دریافت فایل آرتیفکت بکاپ با ثبت شناسه کاربر و IP.
     * `backup.delete`: در زمان حذف دستی یا سیستمی نسخه‌های پشتیبان.

---

## ۶. تصمیمات کلیدی طراحی API (API Design Decisions)

| حوزه تصمیم‌گیری | تصمیم اتخاذشده | دلیل و منطق معماری |
| :--- | :--- | :--- |
| **Authentication Model** | مدل ترکیبی Stateless JWT (برای دسترسی کوتاه‌مدت) به همراه Refresh Token در HttpOnly Secure Cookie و مدیریت سشن در سرور | حفظ تعادل میان عملکرد و استقلال، امنیت بالا در برابر حملات XSS/CSRF، و امکان ابطال فوری (Revocation) سشن از سمت سرور. |
| **Authorization Model** | کنترل دسترسی مبتنی بر نقش سازمانی (Tenant-Scoped RBAC: `admin`, `member`, `viewer`) | سازگاری کامل با معماری چندسازمانی (Multi-tenancy)، ایزولاسیون کامل داده‌ها و آمادگی برای تبدیل به SaaS تجاری با سطوح دسترسی شفاف. |
| **API Versioning Strategy** | نسخه‌گذاری صریح در مسیر URL (`/api/v1/...`) | سادگی در پیاده‌سازی، وضوح برای کلاینت‌ها، امکان توسعه نسخه‌های بعدی (v2) بدون شکستن سازگاری کلاینت‌های قدیمی. |
| **Security Approach** | پنهان‌سازی کامل سکرت‌ها و مسیرها (Path & Credential Non-Disclosure)، استاندارد یکپارچه خطاها، و ثبت لاگ‌های حسابرسی پاک‌سازی‌شده | کاهش حداکثری سطح حمله (Attack Surface)، تطابق کامل با استانداردهای امنیتی سازمانی و حفظ محرمانگی داده‌ها در حالت سکون و تبادل. |

---

## ۷. بخش پنجم: طراحی API مدیریت سازمان‌ها (Organization API)

تمامی داده‌ها و منابع در پلتفرم به یک `Organization` تعلق دارند. در نسخه ۱ با وجود `Internal Organization` پیش‌فرض، ادمین سیستم امکان ایجاد و مدیریت چند سازمان را داراست.

### ۱. فهرست سازمان‌های کاربر (`GET /api/v1/organizations`)
* **هدف**: بازگرداندن لیست تمام سازمان‌هایی که کاربر احراز هویت‌شده عضو آن‌هاست.
* **سطح دسترسی مورد نیاز**: هر کاربر لاگین‌شده (`admin`, `member`, `viewer`).

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": [
    {
      "id": "b1eebc99-9c0b-4ef8-bb6d-6bb9bd380b22",
      "name": "Internal Organization",
      "slug": "internal-org",
      "is_default_internal": true,
      "status": "active",
      "user_role": "admin",
      "created_at": "2026-08-20T10:00:00Z"
    }
  ],
  "message": "فهرست سازمان‌ها با موفقیت دریافت شد.",
  "request_id": "req-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
}
```

---

### ۲. ایجاد سازمان جدید (`POST /api/v1/organizations`)
* **هدف**: ایجاد یک سازمان/مستأجر جدید در پلتفرم.
* **سطح دسترسی مورد نیاز**: منحصراً سوپرادمین کل سیستم (`is_system_admin = true`). مدیران سازمان‌ها در سطح مستأجر مجاز به ایجاد سازمان هم‌سطح جدید نیستند.
* **قوانین اعتبارسنجی**: فیلد `slug` باید در کل سیستم یکتا، با حروف کوچک انگلیسی و خط تیره باشد.

#### ساختار درخواست (Request):
```http
POST /api/v1/organizations HTTP/1.1
Host: api.backup-platform.local
Content-Type: application/json
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

{
  "name": "Acme Corporation",
  "slug": "acme-corp",
  "metadata": {
    "plan": "standard",
    "max_resources": 10
  }
}
```

#### ساختار پاسخ موفق (`201 Created`):
```json
{
  "data": {
    "id": "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380c33",
    "name": "Acme Corporation",
    "slug": "acme-corp",
    "is_default_internal": false,
    "status": "active",
    "created_at": "2026-08-20T11:00:00Z"
  },
  "message": "سازمان با موفقیت ایجاد گردید.",
  "request_id": "req-2b3c4d5e-6f7a-8b9c-0d1e-2f3a4b5c6d7e"
}
```

---

### ۳. دریافت جزئیات سازمان (`GET /api/v1/organizations/{id}`)
* **هدف**: مشاهده مشخصات یک سازمان مشخص.
* **سطح دسترسی مورد نیاز**: عضویت در همان سازمان (`admin`, `member`, `viewer`).

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "id": "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380c33",
    "name": "Acme Corporation",
    "slug": "acme-corp",
    "is_default_internal": false,
    "status": "active",
    "metadata": {
      "plan": "standard",
      "max_resources": 10
    },
    "created_at": "2026-08-20T11:00:00Z",
    "updated_at": "2026-08-20T11:00:00Z"
  },
  "message": "اطلاعات سازمان با موفقیت دریافت شد.",
  "request_id": "req-3c4d5e6f-7a8b-9c0d-1e2f-3a4b5c6d7e8f"
}
```

---

### ۴. ویرایش سازمان (`PUT /api/v1/organizations/{id}`)
* **هدف**: ویرایش نام و تنظیمات متادیتای سازمان.
* **سطح دسترسی مورد نیاز**: نقش `admin` در سازمان مربوطه یا `system_admin`.
* **سیاست حذف و آرشیو**: در نسخه ۱، حذف فیزیکی سازمان‌ها انجام نمی‌شود؛ در صورت نیاز به غیرفعال‌سازی، وضعیت سازمان به `archived` یا `suspended` تغییر می‌یابد تا پیوند تاریخچه Audit Log و داده‌ها حفظ گردد.

#### ساختار درخواست (Request):
```json
{
  "name": "Acme Corp International",
  "metadata": {
    "plan": "enterprise",
    "max_resources": 50
  }
}
```

---

## ۸. بخش ششم: طراحی API مدیریت منابع (Resource Management API)

منابع پشتیبانی‌شده در نسخه ۱:
1. **`ubuntu_ssh`**: سرور لینوکس ابونتو از طریق پروتکل SSH (با دسترسی root).
2. **`cpanel`**: هاست اشتراکی بر پایه cPanel بدون دسترسی root (با اولویت API Token).

### ۱. فهرست منابع سازمان (`GET /api/v1/resources`)
* **هدف**: دریافت لیست کلیه منابع ثبت‌شده در سازمان فعال جاری.
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.
* **محدودسازی نمایش اطلاعات اتصال بر اساس نقش (Role-Based Visibility)**:
  * **نقش `admin`**: دسترسی کامل به کلیه جزئیات پیکربندی، مشخصات اتصال کانکتور (Host, Port, Username)، اثر انگشت کلید سرور (`host_key_fingerprint`) و ارجاع به Credential.
  * **نقش `member`**: مشاهده اطلاعات عملیاتی لازم جهت پایش و اجرای جاب‌ها (نام منبع، نوع، Host، Port، وضعیت اتصال).
  * **نقش `viewer`**: مشاهده صرفاً متادیتای عمومی و غیرحساس (نام، نوع منبع، وضعیت عملیاتی، زمان و نتیجه آخرین تست اتصال) بدون نمایش پارامترهای زیرساختی شبکه یا اطلاعات کانکتور.
* **محرمانگی**: اطلاعات حساس احراز هویت (پسوردها، توکن‌ها، کلیدها) هرگز در پاسخ ارسال نمی‌شوند.

#### ساختار پاسخ موفق (`200 OK` - برای نقش Admin):
```json
{
  "data": [
    {
      "id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
      "name": "Production Database Ubuntu Server",
      "type": "ubuntu_ssh",
      "status": "active",
      "last_connection_test_at": "2026-08-20T11:30:00Z",
      "last_connection_status": "success",
      "connector": {
        "host": "198.51.100.10",
        "port": 22,
        "auth_type": "ssh_key",
        "username": "root",
        "host_key_fingerprint": "SHA256:mQ3F...abc",
        "credential_id": "e4eebc99-9c0b-4ef8-bb6d-6bb9bd380e55",
        "credential_name": "Prod Server SSH Key"
      },
      "created_at": "2026-08-20T11:15:00Z"
    }
  ],
  "message": "فهرست منابع با موفقیت دریافت شد.",
  "request_id": "req-4d5e6f7a-8b9c-0d1e-2f3a-4b5c6d7e8f9a"
}
```

---

### ۲. ایجاد منبع جدید (`POST /api/v1/resources`)
* **هدف**: ثبت یک منبع جدید (`ubuntu_ssh` یا `cpanel`) به همراه کانکتور و اتصال به Credential از پیش تعریف‌شده.
* **سطح دسترسی مورد نیاز**: نقش `admin` در سازمان فعال.

#### ۲-۱. ساختار درخواست برای منبع Ubuntu SSH:
```json
{
  "name": "Ubuntu App & DB Server",
  "type": "ubuntu_ssh",
  "connector": {
    "host": "198.51.100.15",
    "port": 22,
    "auth_type": "ssh_key",
    "username": "root",
    "credential_id": "e4eebc99-9c0b-4ef8-bb6d-6bb9bd380e55",
    "host_key_fingerprint": "SHA256:uN8Q...xyz",
    "config": {
      "connection_timeout_seconds": 15
    }
  }
}
```
* **قوانین امنیتی منبع اوبونتو**:
  * روش احراز هویت کلید اختصاصی SSH (`ssh_key`) بر پسورد اولویت و ترجیح دارد.
  * فیلد `host_key_fingerprint` سرور مقصد برای جلوگیری از حملات مرد میانی (MITM) ذخیره و اعتبارسنجی می‌شود.
  * توصیه می‌شود اتصال قبل از فعال‌سازی با فراخوانی Test Connection بررسی گردد.

#### ۲-۲. ساختار درخواست برای منبع cPanel:
```json
{
  "name": "Main Shared Hosting Account",
  "type": "cpanel",
  "connector": {
    "host": "cpanel.example.com",
    "port": 2083,
    "auth_type": "cpanel_api_token",
    "username": "mycpaneluser",
    "credential_id": "f5eebc99-9c0b-4ef8-bb6d-6bb9bd380f66",
    "config": {
      "use_https": true,
      "connection_timeout_seconds": 20
    }
  }
}
```
* **قوانین امنیتی منبع cPanel**:
  * استفاده از `cpanel_api_token` بر استفاده از پسورد اصلی حساب کاربری اولویت قطعی دارد.
  * پورت پیش‌فرض برای اتصال امن ۲۰۸۳ (cPanel HTTPS) است.

#### ساختار پاسخ موفق (`201 Created`):
```json
{
  "data": {
    "id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
    "name": "Ubuntu App & DB Server",
    "type": "ubuntu_ssh",
    "status": "active",
    "created_at": "2026-08-20T11:45:00Z"
  },
  "message": "منبع با موفقیت ثبت گردید.",
  "request_id": "req-5e6f7a8b-9c0d-1e2f-3a4b-5c6d7e8f9a0b"
}
```

---

### ۳. دریافت مشخصات یک منبع (`GET /api/v1/resources/{id}`)
* **هدف**: مشاهده متادیتا و مشخصات اتصال غیرحساس منبع مشخص با اعمال سطوح نمایش مبتنی بر نقش (Role-Based Visibility).
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.

---

### ۴. ویرایش منبع (`PUT /api/v1/resources/{id}`)
* **هدف**: تغییر نام، پارامترهای شبکه کانکتور یا انتساب به Credential جدید.
* **سطح دسترسی مورد نیاز**: نقش `admin`.

---

### ۵. آرشیو کردن منبع (`DELETE /api/v1/resources/{id}`)
* **هدف**: غیرفعال‌سازی و آرشیو منبع.
* **سطح دسترسی مورد نیاز**: نقش `admin`.
* **ماهیت عملیات در نسخه ۱ (Soft Delete / Archive)**: فراخوانی `DELETE /api/v1/resources/{id}` در نسخه ۱ یک عملیات **Soft Delete / Archive** است و هیچ‌گونه حذف فیزیکی (Hard Delete) در پایگاه داده انجام نمی‌دهد؛ وضعیت منبع به `archived` تغییر یافته و زمان‌بندی‌های خودکار آن متوقف می‌شوند، در حالی که تاریخچه کامل `BackupJobs`, `BackupRuns` و آرتیفکت‌های ایجادشده جهت پیگیری‌های قانونی و بازیابی حفظ می‌گردند.

---

## ۹. بخش هفتم: طراحی API مدیریت دسترسی‌ها و سکرت‌ها (Credential Management API)

صندوقچه نگهداری امن کلیدهای SSH، پسوردها و توکن‌های API به صورت رمزنگاری‌شده با AES-256-GCM.

### ۱. فهرست کردانشال‌های سازمان (`GET /api/v1/credentials`)
* **هدف**: مشاهده لیست اطلاعات هویتی ثبت‌شده بدون نمایش اطلاعات حساس.
* **سطح دسترسی مورد نیاز**: نقش `admin`.
* **قانون قطعی**: هرگز محتوای سکرت یا `encrypted_secret` برگردانده نمی‌شود.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": [
    {
      "id": "e4eebc99-9c0b-4ef8-bb6d-6bb9bd380e55",
      "name": "Prod Server SSH Key",
      "type": "ssh_private_key",
      "fingerprint": "SHA256:7uK...p9X",
      "key_version": 1,
      "created_at": "2026-08-20T10:30:00Z"
    }
  ],
  "message": "فهرست کردانشال‌ها با موفقیت دریافت شد.",
  "request_id": "req-6f7a8b9c-0d1e-2f3a-4b5c-6d7e8f9a0b1c"
}
```

---

### ۲. ثبت کردانشال جدید (`POST /api/v1/credentials`)
* **هدف**: دریافت سکرت در بدنه درخواست از طریق اتصال امن HTTPS، رمزگذاری با AES-256-GCM در لایه سرور، و ذخیره Nonce و Auth Tag.
* **سطح دسترسی مورد نیاز**: نقش `admin`.

#### ساختار درخواست (Request):
```json
{
  "name": "Prod Server SSH Key",
  "type": "ssh_private_key",
  "secret": "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAA...\n-----END OPENSSH PRIVATE KEY-----",
  "passphrase": null
}
```

#### ساختار پاسخ موفق (`201 Created`):
```json
{
  "data": {
    "id": "e4eebc99-9c0b-4ef8-bb6d-6bb9bd380e55",
    "name": "Prod Server SSH Key",
    "type": "ssh_private_key",
    "fingerprint": "SHA256:7uK...p9X",
    "created_at": "2026-08-20T12:00:00Z"
  },
  "message": "اطلاعات هویتی با موفقیت و به صورت امن ذخیره شد.",
  "request_id": "req-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d"
}
```

---

### ۳. ویرایش کردانشال (`PUT /api/v1/credentials/{id}`)
* **هدف**: تغییر نام یا چرخش کلید/سکرت (Key Rotation).
* **سطح دسترسی مورد نیاز**: نقش `admin`.

---

### ۴. حذف کردانشال (`DELETE /api/v1/credentials/{id}`)
* **هدف**: حذف سکرت از دیتابیس.
* **سطح دسترسی مورد نیاز**: نقش `admin`.
* **قید وابستگی**: در صورتی که این Credential در یک Resource فعال استفاده شده باشد، عملیات با خطای `409 Conflict` یا `400 Bad Request` متوقف می‌شود (قید `RESTRICT`).
* **ضوابط ثبت حسابرسی (Audit Rules for Credentials)**:
  * تمامی عملیات Create، Update، Delete و دسترسی/مشاهده دستی توسط ادمین در Audit Log ثبت می‌شوند.
  * **استفاده داخلی Workerها**: فراخوانی و رمزگشایی خودکار سکرت‌ها توسط Workerهای پس‌زمینه در زمان اجرای فرآیندهای روتین بکاپ، رویداد `credential.access` در Audit Log تولید **نمی‌کند** تا از اتلاف ظرفیت دیتابیس و کاهش کارایی سیستم جلوگیری شود.

---

## ۱۰. بخش هشتم: سرویس‌های عملیاتی منابع (Test Connection & Database Discovery APIs)

### ۱. تست اتصال به منبع (`POST /api/v1/resources/{id}/test-connection`)
* **هدف**: اعتبارسنجی زنده اتصال شبکه و اصالت اطلاعات هویتی قبل از فعال‌سازی یا اجرای بکاپ.
* **سطح دسترسی مورد نیاز**: در نسخه ۱ به صورت پیش‌فرض منحصراً نقش **`admin`** است (با قابلیت تفکیک در آینده به Permission مستقل نظیر `resource:test`).
* **مکانیزم اجرا**:
  * **برای Ubuntu**: برقراری هندشیک SSH، احراز هویت با Key/Password، و بررسی اثر انگشت سرور.
  * **برای cPanel**: فراخوانی متد اعتبارسنجی توکن/سشن در cPanel UAPI.
* **امنیت پیام‌های خطا**: خطاهای داخلی کتابخانه‌های SSH یا کدهای خطای وب‌سرور cPanel هرگز به کاربر نشان داده نشده و به پیام‌های عمومی و امن ترجمه می‌شوند.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "status": "success",
    "latency_ms": 42,
    "checked_at": "2026-08-20T12:15:00Z",
    "details": {
      "server_banner": "OpenSSH_8.9p1 Ubuntu-3ubuntu0.6",
      "auth_method": "publickey"
    }
  },
  "message": "اتصال به منبع با موفقیت برقرار شد.",
  "request_id": "req-8b9c0d1e-2f3a-4b5c-6d7e-8f9a0b1c2d3e"
}
```

#### ساختار پاسخ در صورت عدم برقراری اتصال (`200 OK` با وضعیت `failed` یا `422`):
```json
{
  "data": {
    "status": "failed",
    "latency_ms": 3012,
    "checked_at": "2026-08-20T12:15:00Z",
    "details": {
      "reason": "عدم پاسخ‌گویی سرور در مهلت مجاز (Connection Timeout) یا اطلاعات اعتبارسنجی نامعتبر است."
    }
  },
  "message": "برقراری ارتباط با منبع ناموفق بود.",
  "request_id": "req-8b9c0d1e-2f3a-4b5c-6d7e-8f9a0b1c2d3f"
}
```

---

### ۲. شناسایی خودکار دیتابیس‌های MySQL (`GET /api/v1/resources/{id}/databases`)
* **هدف**: لیست‌کردن خودکار پایگاه‌های داده MySQL موجود در سرور یا هاست جهت انتخاب در برنامه پشتیبان‌گیری بدون نیاز به وارد کردن دستی نام دیتابیس‌ها.
* **پایگاه داده هدف در نسخه ۱**: **MySQL**.
* **سطح دسترسی مورد نیاز**: دسترسی این Endpoint از طریق Permission کنترل می‌شود و در نسخه ۱ منحصراً محدود به نقش **`admin`** است (با قابلیت انتساب به پرمیشن مستقل `resource:discover` در مدل SaaS).
* **محرمانگی**: هیچ کردانشال یا متادیتای حساسی در پاسخ ارسال نمی‌شود.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": [
    {
      "name": "ecommerce_prod",
      "size_bytes": 104857600,
      "tables_count": 48,
      "status": "accessible"
    },
    {
      "name": "analytics_dw",
      "size_bytes": 524288000,
      "tables_count": 12,
      "status": "accessible"
    }
  ],
  "message": "فهرست پایگاه‌های داده با موفقیت شناسایی شد.",
  "request_id": "req-9c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f"
}
```

---

## ۱۱. بخش نهم: ضوابط امنیتی و تصمیمات معماری مدیریت منابع (Security Rules & Resource API Decisions)

### قواعد امنیتی اختصاصی منابع و کردانشال‌ها:
1. **ایزولاسیون کامل دامنه‌بندی سازمانی**: تمام عملیات‌های منابع، کانکتورها و کردانشال‌ها در سطح `Organization` فیلتر می‌شوند. تلاش برای دسترسی به منبع یک سازمان دیگر، پاسخ `404 Not Found` در پی دارد.
2. **منع قطعی بازگرداندن سکرت‌ها (Zero Secret Exposure)**: فیلدهای رمز، کلید خصوصی، و توکن‌ها یک‌طرفه (Write-Only) هستند؛ پس از ارسال در متد POST/PUT، فقط در حافظه رمزشده ذخیره می‌شوند و هرگز در هیچ پاسخی برگردانده نخواهند شد.
3. **حفظ تاریخچه با آرشیو منابع (Soft Archiving)**: غیرفعال‌سازی منبع صرفاً وضعیت آن را به `archived` تغییر می‌دهد و هیچ‌یک از رکوردهای Job، Run و Artifactهای گذشته حذف نمی‌شوند.
4. **ثبت کامل لاگ‌های حسابرسی (Audit Logging)**: تمامی عملیات زیر بلافاصله در `audit_logs` ثبت می‌گردند:
   * `resource.create` / `resource.update` / `resource.archive`
   * `credential.create` / `credential.update` / `credential.delete`
   * `credential.access` (منحصراً برای مشاهده یا عملیات دستی کاربر/ادمین؛ بدون ثبت برای اجراهای خودکار Worker)
   * `resource.test_connection`
   * `resource.database_discovery`

### خلاصه تصمیمات کلیدی طراحی API منابع:

| حوزه تصمیم‌گیری | تصمیم اتخاذشده | دلیل و منطق معماری |
| :--- | :--- | :--- |
| **Resource-Connector Decoupling** | تفکیک منبع از کانکتور و ارجاع به موجودیت مستقل `Credential` | امکان استفاده مجدد از کردانشال‌ها، چرخش متمرکز کلیدها (Key Rotation) و عدم افشای مشخصات سرور. |
| **Connection Pre-Validation** | تعبیه Endpoint اختصاصی `test-connection` با زمان پاسخ کوتاه و دسترسی Admin در V1 | اعتبارسنجی پیکربندی پیش از تعریف زمان‌بندی و جلوگیری از شکست غیرمنتظره جاب‌های شبانه بکاپ. |
| **Dynamic MySQL Discovery** | شناسایی خودکار پایگاه‌های داده تحت کنترل پرمیشن Admin بدون دریافت سکرت اضافی | ساده‌سازی رابط کاربری برای ادمین، کاهش خطای انسانی در تایپ نام دیتابیس‌ها و تطابق با دسترسی‌های cPanel UAPI و SSH. |
| **Soft Archiving Policy** | استفاده از سیاست Soft Delete/Archive به جای Hard Delete برای Resource و Organization | حفظ یکپارچگی ارجاعات در دیتابیس و امکان ردیابی قانونی و امنیتی سوابق بکاپ‌های قدیمی. |
| **Role-Based Connection Visibility** | فیلتر کردن جزئیات اتصال کانکتورها بر اساس نقش کاربر (`admin`, `member`, `viewer`) | رعایت اصل حداقل دسترسی (Least Privilege) و جلوگیری از افشای توپولوژی شبکه به نقش‌های غیرادمین. |

---

## ۱۲. بخش دهم: طراحی API برنامه‌های پشتیبان‌گیری و زمان‌بندی (Backup Plan & Scheduling API)

موجودیت `BackupPlan` تعریف‌کننده خط‌مشی دائمی و پایدار پشتیبان‌گیری برای یک منبع است که نوع، منابع داده (دیتابیس/فایل)، الگوی زمان‌بندی و سیاست نگهداری را مشخص می‌کند.

### ۱. فهرست برنامه‌های پشتیبان‌گیری (`GET /api/v1/backup-plans`)
* **هدف**: نمایش لیست پلن‌های تعریف‌شده درون سازمان فعال جاری.
* **فیلترهای پشتیبانی‌شده در Query Parameters**:
  * `resource_id`: فیلتر پلن‌های مرتبط با یک منبع خاص (`UUID`).
  * `status`: وضعیت پلن (`active`, `paused`, `archived`).
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": [
    {
      "id": "7f1eeb99-9c0b-4ef8-bb6d-6bb9bd380a77",
      "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
      "resource_name": "Production Database Ubuntu Server",
      "name": "Daily MySQL Backup Plan",
      "backup_type": "mysql_database",
      "status": "active",
      "database_selection": {
        "mode": "selected",
        "databases": ["ecommerce_prod", "analytics_dw"]
      },
      "schedule": {
        "is_enabled": true,
        "cron_expression": "0 2 * * *",
        "timezone": "Asia/Tehran",
        "next_run_at": "2026-08-21T02:00:00+03:30"
      },
      "retention_policy": {
        "keep_last_n": 7,
        "keep_days": 30
      },
      "created_at": "2026-08-20T12:00:00Z"
    }
  ],
  "message": "فهرست برنامه‌های پشتیبان‌گیری با موفقیت دریافت شد.",
  "request_id": "req-1a1a2b2b-3c3c-4d4d-5e5e-6f6f7a7a8b8b"
}
```

---

### ۲. ایجاد برنامه پشتیبان‌گیری جدید (`POST /api/v1/backup-plans`)
* **هدف**: ثبت یک برنامه پشتیبان‌گیری جدید برای یک منبع سازمانی.
* **سطح دسترسی مورد نیاز**: نقش `admin`.
* **انواع پشتیبان‌گیری در نسخه ۱ (Backup Types)**:
  * `mysql_database`: استخراج پایگاه‌های داده MySQL با `mysqldump` یا ابزار بومی cPanel.
  * `website_files`: فشرده‌سازی و آرشیو مسیر فایل‌های وب‌سایت (مانند `/var/www` یا `public_html`).
* **قوانین اعتبارسنجی داده‌ها (Validation Rules)**:
  * `resource_id` باید متعلق به همان سازمان فعال درخواست‌کننده باشد (Cross-Org Check).
  * `database_selection` تنها برای نوع `mysql_database` معتبر و الزامی است (حالت `all` یا لیست اسامی دیتابیس‌ها).
  * `file_selection` فقط برای نوع `website_files` معتبر بوده و نیازمند مسیرهای مبدأ معتبر (`paths`) و الگوهای استثنا (`exclude_patterns`) است.
  * **اعتبارسنجی زمان‌بندی (Scheduling Validation)**:
    * در صورت `is_enabled = true`، فیلد `cron_expression` الزامی بوده و باید سینتکس ۵-بخشی استاندارد Cron را پاس کند.
    * فیلد `timezone` باید یک شناسه معتبر پایگاه داده IANA (نظیر `UTC` یا `Asia/Tehran`) باشد.

#### ساختار درخواست برای ایجاد Plan دیتابیس (Request):
```json
{
  "name": "Nightly MySQL Backup",
  "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
  "backup_type": "mysql_database",
  "database_selection": {
    "mode": "selected",
    "databases": ["ecommerce_prod"]
  },
  "schedule": {
    "is_enabled": true,
    "cron_expression": "0 2 * * *",
    "timezone": "Asia/Tehran"
  },
  "retention_policy": {
    "keep_last_n": 14,
    "keep_days": 30
  }
}
```

#### ساختار پاسخ موفق (`201 Created`):
```json
{
  "data": {
    "id": "7f1eeb99-9c0b-4ef8-bb6d-6bb9bd380a77",
    "name": "Nightly MySQL Backup",
    "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
    "status": "active",
    "created_at": "2026-08-20T12:30:00Z"
  },
  "message": "برنامه پشتیبان‌گیری با موفقیت ثبت گردید.",
  "request_id": "req-2b2b3c3c-4d4d-5e5e-6f6f-7a7a8b8b9c9c"
}
```

---

### ۳. دریافت مشخصات برنامه پشتیبان‌گیری (`GET /api/v1/backup-plans/{id}`)
* **هدف**: مشاهده مشخصات، زمان‌بندی و سیاست نگهداری یک Plan خاص.
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.

---

### ۴. ویرایش برنامه پشتیبان‌گیری (`PUT /api/v1/backup-plans/{id}`)
* **هدف**: تغییر نام، زمان‌بندی، دیتابیس‌های هدف یا سیاست نگهداری.
* **سطح دسترسی مورد نیاز**: نقش `admin`.

---

### ۵. آرشیو برنامه پشتیبان‌گیری (`DELETE /api/v1/backup-plans/{id}`)
* **هدف**: غیرفعال‌سازی دائمی و توقف زمان‌بندی Plan.
* **سطح دسترسی مورد نیاز**: نقش `admin`.
* **سیاست عدم حذف فیزیکی**: در نسخه ۱، حذف فیزیکی انجام نمی‌شود؛ وضعیت به `archived` تغییر می‌یابد و Scheduler اجرای آن را متوقف می‌کند، ولی تاریخچه کامل جاب‌ها و آرتیفکت‌های مرتبط حفظ می‌گردد.

---

## ۱۳. بخش یازدهم: طراحی API جاب‌ها و اجرای پشتیبان‌گیری (Backup Jobs & Execution API)

موجودیت `BackupJob` نشان‌دهنده یک درخواست منطقی برای اجرای پشتیبان‌گیری است که می‌تواند بر اساس یک `BackupPlan` زمان‌بندی‌شده ایجاد شده باشد، یا به صورت مستقل و دستی (`Manual Backup`) ثبت گردد.

### ۱. ایجاد جاب پشتیبان‌گیری دستی (`POST /api/v1/backup-jobs`)
* **هدف**: ثبت یک درخواست منطقی پشتیبان‌گیری در صف پایدار پایگاه داده (`Durable Queue`).
* **رفتار لایه HTTP**: کنترلر HTTP صرفاً رکورد `BackupJob` را با وضعیت `pending` در دیتابیس ثبت و تثبیت (Commit) می‌کند و فوراً پاسخ می‌دهد. کنترلر HTTP هرگز اقدام به ایجاد `BackupRun`، اتصال به کانکتور، یا اجرای فرآیند بکاپ به صورت همگام نمی‌نماید؛ Worker پس از تصاحب جاب از صف، `BackupRun` را ایجاد و اجرا می‌کند.
* **قوانین تفکیک سطوح دسترسی (RBAC Enforcement)**:
  * **نقش `admin`**: مجاز به ایجاد جاب از طریق ارجاع به `backup_plan_id` یا به صورت سفارشی (Ad-hoc) با ارسال مستقیم `resource_id`، `backup_type` و `target_spec`.
  * **نقش `member`**: **صرفاً** مجاز به تحریک اجرای یک Plan فعال و از پیش تاییدشده در همان سازمان با ارسال `backup_plan_id`. کاربر با نقش Member حق ارسال `target_spec` دلخواه، تعیین دیتابیس یا مسیر فایل‌سیستم سفارشی، یا تغییر اهداف تعریف‌شده در Plan را ندارد.
  * **نقش `viewer`**: فاقد دسترسی ایجاد یا اجرای جاب.
* **اسنپ‌شات قطعی تنظیمات (`target_spec Snapshot`)**: در صورت ایجاد جاب از روی Plan، تنظیمات Plan در لحظه ایجاد جاب در فیلد `target_spec` اسنپ‌شات می‌شود تا تغییرات آتی Plan روی جاب‌های ثبت‌شده اثر نگذارد.
* **وضعیت‌های مجاز جاب (Canonical Job Statuses)**:
  * `pending`: جاب در صف پایدار PostgreSQL ثبت شده و آماده تصاحب توسط کارگر است.
  * `running`: کارگر پردازش جاب را در قالب تلاش اجرایی فعال (`BackupRun`) آغاز کرده است.
  * `completed`: عملیات پشتیبان‌گیری با موفقیت کامل خاتمه یافته است.
  * `failed`: اجرای جاب با خطا متوقف گردیده است.
  * `cancelled`: جاب در وضعیت `pending` پیش از شروع اجرا لغو شده است.

#### ساختار درخواست ایجاد Job دستی توسط ادمین (Request - Ad-hoc):
```http
POST /api/v1/backup-jobs HTTP/1.1
Host: api.backup-platform.local
Content-Type: application/json
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

{
  "backup_plan_id": null,
  "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
  "backup_type": "mysql_database",
  "target_spec": {
    "databases": ["ecommerce_prod"]
  }
}
```

#### ساختار درخواست اجرای Plan توسط Member یا Admin (Request - From Plan):
```http
POST /api/v1/backup-jobs HTTP/1.1
Host: api.backup-platform.local
Content-Type: application/json
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

{
  "backup_plan_id": "7a1eeb99-9c0b-4ef8-bb6d-6bb9bd380a77"
}
```

#### ساختار پاسخ موفق (`202 Accepted`):
```json
{
  "data": {
    "id": "8a2eeb99-9c0b-4ef8-bb6d-6bb9bd380b88",
    "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
    "backup_plan_id": null,
    "backup_type": "mysql_database",
    "target_spec": {
      "databases": ["ecommerce_prod"]
    },
    "status": "pending",
    "trigger_type": "manual",
    "created_at": "2026-08-20T13:00:00Z"
  },
  "message": "درخواست پشتیبان‌گیری با موفقیت در صف پایدار قرار گرفت.",
  "request_id": "req-3c3c4d4d-5e5e-6f6f-7a7a-8b8b9c9c0d0d"
}
```

---

### ۲. فهرست تاریخچه اجراهای پشتیبان‌گیری (`GET /api/v1/backup-runs`)
* **هدف**: دریافت سوابق تلاش‌های اجرایی گذشته به همراه متادیتا و زمان‌بندی‌ها.
* **فیلترهای پشتیبانی‌شده (Query Parameters)**:
  * `resource_id`: فیلتر بر اساس شناسه منبع.
  * `job_id`: فیلتر بر اساس شناسه جاب منطقی.
  * `status`: وضعیت اجرا (`pending`, `running`, `success`, `failed`, `cancelled`).
  * `from_date` و `to_date`: بازه زمانی اجرای عملیات (فرمت ISO-8601).
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.

---

### ۳. دریافت جزئیات یک اجرای مشخص (`GET /api/v1/backup-runs/{id}`)
* **هدف**: مشاهده جزئیات کامل یک اجرای خاص شامل مدت زمان، حجم خروجی تجمیعی، لاگ وضعیت و خطاهای پاک‌سازی‌شده.
* **محاسبه مقادیر مشتق‌شده (Derived Values)**:
  * `total_artifact_size_bytes`: مجموع حجم بایت آرتیفکت‌های حاصل از این ران (محاسبه‌شده از `backup_artifacts`).
  * `duration_seconds`: مدت زمان اجرای عملیات بر حسب ثانیه (محاسبه‌شده بر اساس `ended_at - started_at`).
* **محرمانگی**: هیچ سکرت، پسورد یا مسیر حساسی در پیام خطای بازگردانده‌شده قرار ندارد.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "id": "9b3eeb99-9c0b-4ef8-bb6d-6bb9bd380c99",
    "job_id": "8a2eeb99-9c0b-4ef8-bb6d-6bb9bd380b88",
    "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
    "attempt_number": 1,
    "status": "success",
    "started_at": "2026-08-20T13:05:00Z",
    "ended_at": "2026-08-20T13:07:30Z",
    "duration_seconds": 150,
    "total_artifact_size_bytes": 104857600,
    "error_message": null,
    "artifacts_count": 1,
    "created_at": "2026-08-20T13:05:00Z"
  },
  "message": "اطلاعات اجرای پشتیبان‌گیری با موفقیت دریافت شد.",
  "request_id": "req-5e5e6f6f-7a7a-8b8b-9c9c-0d0d1e1e2f2f"
}
```

---

## ۱۴. بخش دوازدهم: طراحی API آرتیفکت‌ها، دانلود، اعتبارسنجی و نگهداری (Artifacts, Verification & Retention API)

موجودیت `BackupArtifact` خروجی ملموس و فیزیکی تولیدشده در پایان یک Run موفق (نظیر فایل‌های `.sql.gz` یا `.tar.gz`) است.

### ۱. فهرست آرتیفکت‌های پشتیبان‌گیری (`GET /api/v1/backup-artifacts`)
* **هدف**: دریافت لیست فایل‌های خروجی پشتیبان‌گیری متعلق به سازمان فعال.
* **سطح دسترسی مورد نیاز**: `admin`, `member`, `viewer`.

---

### ۲. دریافت مشخصات آرتیفکت (`GET /api/v1/backup-artifacts/{id}`)
* **هدف**: مشاهده مشخصات فنی آرتیفکت شامل نام منطقی، سایز، Checksum، وضعیت اعتبارسنجی و الگوریتم فشرده‌سازی.
* **پنهان‌سازی مسیر فیزیکی (Path Non-Disclosure)**: فیلد `storage_reference` داخلی سرور هرگز در خروجی API ارسال نمی‌شود.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "id": "a4eebc99-9c0b-4ef8-bb6d-6bb9bd380daa",
    "run_id": "9b3eeb99-9c0b-4ef8-bb6d-6bb9bd380c99",
    "resource_id": "d3eebc99-9c0b-4ef8-bb6d-6bb9bd380d44",
    "artifact_name": "mysql_ecommerce_prod_20260820_130500.sql.gz",
    "size_bytes": 104857600,
    "checksum_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "compression_type": "gzip",
    "verification_status": "verified",
    "verified_at": "2026-08-20T13:08:00Z",
    "created_at": "2026-08-20T13:07:30Z"
  },
  "message": "اطلاعات آرتیفکت با موفقیت دریافت شد.",
  "request_id": "req-6f6f7a7a-8b8b-9c9c-0d0d-1e1e2f2f3a3a"
}
```

---

### ۳. دانلود فایل پشتیبان (`GET /api/v1/backup-artifacts/{id}/download`)
* **هدف**: دریافت امن استریم فایل آرتیفکت بکاپ توسط کاربر مجاز.
* **سطح دسترسی مورد نیاز**: `admin`, `member`.
* **ضوابط امنیتی دانلود**:
  1. **احراز هویت و مجوزدهی پیش از ارسال**: کاربر باید توکن معتبر با نقش مجاز در سازمان مالک آرتیفکت را ارائه دهد.
  2. **استریم مستقیم از طریق برنامه (Application Authorization Stream)**: فایل مستقیماً در قالب Stream با هدرهای `Content-Disposition: attachment; filename="..."` و `Content-Type: application/gzip` به کلاینت تحویل می‌شود بدون افشای مسیر واقعی یا ارائه لینک مستقیم فایل‌سیستم.
  3. **ثبت اجباری در Audit Log**: هر فراخوانی موفق دانلود بلافاصله رخداد `backup.download` را همراه با `user_id`، `artifact_id`، `ip_address` و `user_agent` ثبت می‌کند.

---

### ۴. حذف آرتیفکت پشتیبان (`DELETE /api/v1/backup-artifacts/{id}`)
* **هدف**: حذف کنترل‌شده فایل فیزیکی پشتیبان و آزادسازی فضای ذخیره‌سازی.
* **سطح دسترسی مورد نیاز**: منحصراً نقش **`admin`**.
* **مکانیزم و ترتیب اجرا (Physical Deletion First & Tombstone)**:
  1. سیستم ابتدا از طریق `StorageProvider.DeleteArtifact(...)` فایل فیزیکی را از مقصد ذخیره‌سازی حذف می‌کند.
  2. **تنها در صورت موفقیت حذف فیزیکی**، وضعیت متادیتا در دیتابیس به عنوان Tombstone به‌روزرسانی شده و فیلدهای `is_deleted = true` و `deleted_at = NOW()` مقداردهی می‌شوند.
  3. در صورت شکست در حذف فیزیکی از دیسک/استوریج، متادیتای آرتیفکت هرگز به عنوان حذف‌شده علامت نمی‌خورد تا ناهمگونی رخ ندهد.
  4. سوابق و تاریخچه `BackupJob` و `BackupRun` هرگز توسط عملیات حذف آرتیفکت پاک نمی‌شوند و جهت سوابق عملیاتی و حسابرسی حفظ می‌گردند.
  5. ثبت اجباری رویداد `backup.delete` در جدول `audit_logs`.

---

### ۵. اعتبارسنجی صحت فایل پشتیبان (`POST /api/v1/backup-runs/{id}/verify`)
* **هدف**: بررسی برخط و مستقل سلامت فیزیکی و ساختاری آرتیفکت ایجادشده بدون انجام Restore کامل.
* **سطح دسترسی مورد نیاز**: `admin`, `member`.
* **محدوده اعتبارسنجی (Verification Scope in V1)**:
  1. **بررسی Checksum (SHA-256 Validation)**: تطبیق مجدد هش فایل موجود در Storage با هش ثبت‌شده هنگام تولید.
  2. **بررسی یکپارچگی فایل (Archive Integrity Check)**: تست سالم بودن هدرها و بلاک‌های فشرده‌سازی شده (اجرای `gzip -t` یا `tar -tzf`).
  3. **بررسی کامل بودن داده‌ها (Sanity & Size Check)**: بررسی عدم تهی بودن فایل (`size > 0`) و ساختار پایه خروجی دامپ.
  * *توضیح*: اعتبارسنجی V1 سلامت ساختاری آرتیفکت را تضمین می‌کند اما ادعای ضمانت ۱۰۰٪ بازیابی منطقی دیتابیس را ندارد؛ تست کامل بازیابی (Full Restore Test) به فازهای آتی موکول شده است.

#### ساختار پاسخ موفق (`200 OK`):
```json
{
  "data": {
    "run_id": "9b3eeb99-9c0b-4ef8-bb6d-6bb9bd380c99",
    "verification_status": "verified",
    "verified_at": "2026-08-20T13:10:00Z",
    "details": {
      "checksum_matched": true,
      "archive_integrity": "passed",
      "compression_valid": true,
      "extracted_sample_check": "valid_sql_dump"
    }
  },
  "message": "صحت و یکپارچگی ساختاری فایل پشتیبان تأیید گردید.",
  "request_id": "req-7a7a8b8b-9c9c-0d0d-1e1e-2f2f3a3a4b4b"
}
```

---

### ۶. خط‌مشی و مدیریت نگهداری (Retention Management)
* **پارامترهای قابل تنظیم در Plan**:
  * `retention_count`: نگهداری حداکثر N نسخه اخیر از بکاپ‌ها.
  * `retention_days`: نگهداری بکاپ‌ها برای بازه زمانی مشخص (به روز).
* **معناشناسی دقیق ترکیب سیاست‌ها (Retention Semantics)**:
  * در صورتی که هر دو پارامتر `retention_count = N` و `retention_days = D` تنظیم شده باشند، سیاست به صورت محافظه‌کارانه (**Conservative OR**) اعمال می‌شود:
  * **یک آرتیفکت حفظ می‌شود اگر**: جزو N نسخه اخیر باشد **یا (OR)** سن آن کمتر از D روز باشد.
  * **یک آرتیفکت مشمول حذف است تنها زمانی که**: هم خارج از N نسخه اخیر باشد **و هم (AND)** قدیمی‌تر از D روز باشد.
* **قوانین و فرآیند اجرای Retention**:
  * پایش و اعمال خط‌مشی‌های نگهداری به صورت غیرهمگام توسط Background Worker در پایان هر Run موفق یا طی وظایف دوره‌ای پاک‌سازی انجام می‌شود.
  * حذف فیزیکی بایت‌ها از استوریج پیش از علامت‌گذاری Tombstone در دیتابیس الزامی است؛ تاریخچه Job و Run هرگز حذف نمی‌شود.
  * هرگونه حذف خودکار ناشی از اعمال سیاست Retention، با تولید رویداد `retention.cleanup` در Audit Log ثبت می‌شود.

---

## ۱۵. بخش سیزدهم: ضوابط امنیتی و تصمیمات معماری چرخه بکاپ (Backup API Security Rules & Decisions)

### ضوابط امنیتی حاکم بر کلیه APIهای بکاپ:
1. **ایزولاسیون سازمانی بدون نشت (Tenant Scoping)**: تمام مسیرهای Plan، Job، Run، Artifact و دانلودها ملزم به داشتن کانتکست سازمان فعال هستند؛ هیچ کلاینتی قادر به مشاهده یا دانلود آرتیفکت‌های سازمان دیگر نیست.
2. **عدم افشای سکرت‌ها و خطاهای خام (Information Non-Disclosure)**: خطاهای ابزارهای خارجی (`mysqldump`, `tar`, `ssh`, `cpanel uapi`) پاک‌سازی می‌شوند تا جزئیات حساس دیتابیس یا خطای سیستم‌عامل به کلاینت نرسد.
3. **عدم ثبت رویدادهای غیرضروری برای Workerها**: دسترسی و استفاده دوره‌ای کارگرهای پشتیبان‌گیری به سکرت‌ها و فایل‌ها منجر به تولید بی‌رویه `credential.access` نمی‌شود تا حجم لاگ‌ها کنترل گردد.
4. **حسابرسی اجباری دانلود و حذف**: تمام دانلودها (`backup.download`) و حذف‌ها (`backup.delete`) با مشخصات کامل کاربر و IP در `audit_logs` نگهداری می‌شوند.

### جدول خلاصه تصمیمات کلیدی طراحی API چرخه بکاپ:

| حوزه تصمیم‌گیری | تصمیم اتخاذشده | دلیل و منطق معماری |
| :--- | :--- | :--- |
| **Backup Lifecycle Model** | تفکیک دقیق ۳ لایه: Plan (سیاست) ➔ Job (درخواست منطقی) ➔ Run (تلاش اجرایی واقعی) | امکان ثبت تلاش‌های مجدد (Retry) بدون از دست رفتن رکورد جاب، و پشتیبانی شفاف از اجرای دستی یا زمان‌بندی‌شده. |
| **Manual vs Scheduled Backup** | پشتیبانی از تعریف مستقیم جاب دستی بدون نیاز به ایجاد Backup Plan پیش‌فرض | انعطاف‌پذیری عملیاتی برای ادمین‌ها جهت اخذ بکاپ فوری پیش از اعمال تغییرات یا آپدیت‌های حساس سیستم. |
| **Artifact Security Model** | تحویل استریم از طریق لایه برنامه، پنهان‌سازی مسیر فایل‌سیستم سرور، و اجبار مجوزدهی در لحظه دانلود | حفاظت کامل از فایل‌های حیاتی سازمان و جلوگیری از دسترسی مستقیم، افشای ساختار دیسک یا دور زدن سیستم کنترل دسترسی. |
| **Retention Strategy** | اعمال ترکیبی `retention_count` و `retention_days` با حذف کنترل‌شده توسط Worker و ثبت در لاگ | جلوگیری از پر شدن دیسک سرور در گذر زمان، خودکارسازی انطباق با قوانین نگهداری داده و حفظ ردپای امنیتی کلیه پاک‌سازی‌ها. |
| **Verification Approach** | تأیید درجا با هش SHA-256 و تست سلامت آرشیو (`gzip -t` / `tar -tzf`) بدون تحمیل بار Restore کامل | اطمینان از سلامت فایل پشتیبان در لحظه تولید با حداقل مصرف پردازنده و رم، پیش از مواجهه با بحران بازیابی. |

---

## ۱۶. بخش چهاردهم: طراحی APIهای عملیاتی و سلامت سیستم (Operational Health API)

### دریافت وضعیت سلامت و آمادگی سرویس (`GET /api/v1/health`)
* **هدف**: بررسی وضعیت سلامت کلی سرویس، اتصال به دیتابیس PostgreSQL و آمادگی پذیرش ترافیک (Readiness & Liveness Probe).
* **سطح دسترسی و احراز هویت**: **فاقد نیاز به احراز هویت (Unauthenticated)**. این مسیر برای سیستم‌های مانیتورینگ، لودبالانسر، و فرآیند استقرار در دسترس است.
* **ضوابط اکید امنیتی و عدم افشای اطلاعات (Zero Infrastructure Exposure)**:
  * این مسیر هرگز آدرس Host، پورت دیتابیس، نام کاربری یا کلمه عبور دیتابیس را نمایش نمی‌دهد.
  * هیچ‌گونه مسیر فایل‌سیستم داخلی، متغیرهای محیطی (Environment Variables)، لاگ خطا، Stack Trace یا نسخه دیتابیس/نرم‌افزار در خروجی این مسیر افشا نمی‌شود.

#### ساختار درخواست (Request):
```http
GET /api/v1/health HTTP/1.1
Host: api.backup-platform.local
```

#### ساختار پاسخ موفق (`200 OK` - وضعیت سالم):
```json
{
  "status": "ok"
}
```

#### ساختار پاسخ خطا (`503 Service Unavailable` - عدم اتصال به دیتابیس یا خطای بحرانی):
```json
{
  "status": "unavailable"
}
```



