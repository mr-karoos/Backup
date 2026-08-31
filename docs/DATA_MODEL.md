# مدل داده و ساختار پایگاه داده (Data Model)

این سند طراحی جامع ساختار داده‌ها، موجودیت‌ها (Entities)، روابط، کلیدها و الزامات ذخیره‌سازی پایگاه داده `PostgreSQL` را بر اساس اسناد [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md) و [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md) تدوین می‌کند.

---

## ۱. اصول و قواعد کلی طراحی مدل داده

* **شناسه‌های یکتا (Primary Keys)**: تمامی موجودیت‌ها از کلید اصلی از نوع `UUID` (نسخه ۴ یا استاندارد تصادفی امن) استفاده می‌کنند تا پیش‌بینی‌ناپذیر بوده و برای مقیاس‌پذیری و مهاجرت در محیط‌های توزیع‌شده مناسب باشند.
* **جداسازی کاربر از سازمان (Global Users & Multi-tenancy)**: کاربر (`User`) یک هویت مستقل سراسری است؛ ارتباط میان کاربر و سازمان از طریق جدول رابط `organization_members` برقرار می‌شود تا یک کاربر بتواند عضو و مدیر چند سازمان باشد.
* **دامنه‌بندی سازمانی (Organization Scoping)**: تمامی موجودیت‌های متعلق به مستأجر نظیر `resources`، `resource_connectors`، `credentials`، `backup_plans`، `backup_jobs`، `backup_runs`، `backup_artifacts` و `storage_targets` دارای ستون `organization_id` هستند؛ در حالی که موجودیت‌های سراسری کاربری نظیر `users` و `user_sessions` در سطح سیستم تعریف می‌شوند.
* **منع قطعی ارجاع بین‌سازمانی (Strict Cross-Organization Prevention)**: در تمامی روابط سلسله‌مراتبی، `organization_id` فرزندان باید با `organization_id` والدین یکسان باشد. این سازگاری در لایه Data Layer و با استفاده از قیدهای پایگاه داده تضمین می‌شود.
* **رمزنگاری در حالت سکون (Encryption at Rest)**: فیلدهای حساس در جدول `credentials` با الگوریتم **AES-256-GCM** همراه با Nonce، Auth Tag و نسخه کلید نگهداری می‌شوند و هیچ سکرتی Plaintext ذخیره نمی‌شود.
* **عدم وابستگی مسیر فایل به کاربر (Path Non-Disclosure)**: در `backup_artifacts` صرفاً ارجاع و شناسه ذخیره‌سازی داخلی (`storage_reference`) ثبت می‌شود و مسیرهای فیزیکی فایل‌سیستم سرور هرگز به کاربر نمایش داده نمی‌شوند.
* **ثبت وقایع به صورت Append-Oriented**: موجودیت `audit_logs` صرفاً به صورت افزایشی وقایع حساس را ثبت می‌کند و هرگز اطلاعات حساس (رمز، توکن، کلید خصوصی) در آن درج نمی‌شود.

---

## ۲. دیاگرام کلی روابط موجودیت‌ها (Entity Relationship Overview)

```text
+-----------------------+           1:N           +----------------------------+
|     organizations     | ----------------------> |    organization_members    |
|-----------------------|                         |----------------------------|
| id (UUID, PK)         |                         | id (UUID, PK)              |
| name, slug            |                         | organization_id (UUID, FK) | <----+
| is_default_internal   |                         | user_id (UUID, FK)         |      |
+-----------------------+                         | role                       |      |
       |     |     |                              +----------------------------+      |
       |     |     | 1:N                                                              |
       |     |     +-------------------------------------------------+                |
       |     |                                                       |                |
       |     | 1:N                                                   v                |
       |     v                                         +----------------------------+ |
       | +----------------------------+       N:1      |        credentials         | |
       | |      storage_targets       | <--------------+----------------------------| |
       | |----------------------------| (Future S3     | id (UUID, PK)              | |
       | | id (UUID, PK)              |  Credentials)  | organization_id (UUID, FK) | |
       | | organization_id (UUID, FK) |                | encrypted_secret (AES-GCM) | |
       | | type (local/s3), status    |                +----------------------------+ |
       | +----------------------------+                               |               |
       |   |                                                          |               |
       |   | 1:N                                                      |               |
       |   |                                                          |               |
       | 1:N                                                          |               |
       v   |                                                          |               |
+---------------+                                                     |               |
|   resources   | --------------------+ 1:1                           |               |
|---------------|                     v                               |               |
| id (UUID, PK) |          +-----------------------------+            |               |
| org_id (FK)   |          |     resource_connectors     |            |               |
| name, type    |          |-----------------------------|            |               |
+---------------+          | id (UUID, PK)               |            |               |
  |         |              | resource_id (UUID, FK, UNQ) |            |               |
  | 1:N     | 1:N          | credential_id (UUID, FK) ---+------------+               |
  |         |              | host, port, auth_type, cfg  |                            |
  |         |              +-----------------------------+                            |
  |         |                                                                         |
  |         v                                                                         |
  |  +-----------------------------+                                                  |
  |  |        backup_plans         |                                                  |
  |  |-----------------------------|                                                  |
  |  | id (UUID, PK)               |                                                  |
  |  | organization_id (UUID, FK)  |                                                  |
  |  | resource_id (UUID, FK)      |                                                  |
  |  | schedule_cron, timezone     |                                                  |
  |  +-----------------------------+                                                  |
  |         |                                                                         |
  |         | 0..1:N (Nullable FK for Manual Jobs)                                    |
  v         v                                                                         |
+-----------------------------+                                                       |
|         backup_jobs         |                                                       |
|-----------------------------|                                                       |
| id (UUID, PK)               |                                                       |
| organization_id (UUID, FK)  |                                                       |
| resource_id (UUID, FK)      |                                                       |
| backup_plan_id (UUID, FK)   |                                                       |
| trigger_type, status        |                                                       |
+-----------------------------+                                                       |
       |                                                                              |
       | 1:N (Retries - ON DELETE RESTRICT)                                           |
       v                                                                              |
+-----------------------------+                                                       |
|         backup_runs         |                                                       |
|-----------------------------|                                                       |
| id (UUID, PK)               |                                                       |
| organization_id (UUID, FK)  |                                                       |
| job_id (UUID, FK)           |                                                       |
| attempt_number, status      |                                                       |
| started_at, heartbeat_at    | (Crash Recovery Fields)                               |
| lease_until, error_message  |                                                       |
+-----------------------------+                                                       |
       |                                                                              |
       | 1:N (Multi-Artifact - ON DELETE RESTRICT)                                    |
       v                                                                              |
+-----------------------------+                                                       |
|      backup_artifacts       |                                                       |
|-----------------------------|                                                       |
| id (UUID, PK)               |                                                       |
| organization_id (UUID, FK)  |                                                       |
| run_id (UUID, FK)           |                                                       |
| storage_target_id (UUID, FK)+<------------------------------------------------------+
| artifact_type, storage_ref  |
| size_bytes, checksum_hash   |
| verification_status         |
+-----------------------------+
       ^
       | (Audit Trail Events)
+-----------------------------+           0..1:N          +----------------------------+
|         audit_logs          | <------------------------ |         users              |
|-----------------------------|                           |----------------------------|
| id (UUID, PK)               |                           | id (UUID, PK)              |
| organization_id (UUID, FK)  |                           | email                      |
| user_id (UUID, FK)          |                           | password_hash              |
| action, entity_type, entity |                           +----------------------------+
| ip_address, metadata        |                                      |
+-----------------------------+                                      | 1:N
                                                                     v
                                                          +----------------------------+
                                                          |       user_sessions        |
                                                          |----------------------------|
                                                          | id (UUID, PK)              |
                                                          | user_id (UUID, FK)         |
                                                          | refresh_token_hash (UNQ)   |
                                                          | expires_at, revoked_at     |
                                                          +----------------------------+
```

---

## ۳. موجودیت‌های هویتی، سازمانی و منابع (Identity, Organization & Resource Entities)

### ۱. موجودیت سازمان‌ها (`organizations`)

* **وظیفه**: نماینده یک واحد سازمانی/مستأجر (Tenant). در نسخه ۱ با یک سازمان داخلی پیش‌فرض (`Internal Organization`) ایجاد می‌شود و ادمین می‌تواند سازمان‌های دیگر را ایجاد و مدیریت کند.
* **دامنه‌بندی سازمانی**: ریشه دامنه‌بندی سازمانی.

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `name` | `VARCHAR(100)` | نام سازمان (اجباری - مثال: "Internal Organization"). |
| `slug` | `VARCHAR(100)` | شناسه یکتا و متنی سازمان برای آدرس‌ها و تفکیک سیستمی (Unique). |
| `is_default_internal` | `BOOLEAN` | نشان‌دهنده سازمان پیش‌فرض سیستمی در نسخه ۱ (پیش‌فرض: false). |
| `status` | `VARCHAR(20)` | وضعیت سازمان (`active`، `suspended`، `archived` - پیش‌فرض: `active`). |
| `metadata` | `JSONB` | تنظیمات سازمانی و اطلاعات تکمیلی SaaS (مانند سقف مجاز منابع/حجم ذخیره‌سازی، سطح اشتراک، تنظیمات اختصاصی). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد رکورد. |
| `updated_at` | `TIMESTAMPTZ` | زمان آخرین به‌روزرسانی رکورد. |

---

### ۲. موجودیت کاربران (`users`)

* **وظیفه**: نگهداری هویت و اطلاعات حساب کاربری اشخاص (ادمین‌ها و کاربران سیستم). این موجودیت هویت سراسری (Global) دارد و وابسته به یک سازمان خاص نیست.
* **دامنه‌بندی سازمانی**: ندارد (Global Entity).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `email` | `VARCHAR(255)` | آدرس ایمیل کاربر برای لاگین (اجباری، یکتا، به صورت حروف کوچک). |
| `password_hash` | `VARCHAR(255)` | هش غیرقابل بازگشت کلمه عبور با الگوریتم استاندارد (Argon2id یا bcrypt). |
| `full_name` | `VARCHAR(100)` | نام و نام خانوادگی کاربر. |
| `is_system_admin` | `BOOLEAN` | مشخص‌کننده دسترسی سوپرادمین به کل پلتفرم (پیش‌فرض: false). |
| `status` | `VARCHAR(20)` | وضعیت حساب کاربری (`active`، `inactive`، `blocked` - پیش‌فرض: `active`). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد حساب کاربری. |
| `updated_at` | `TIMESTAMPTZ` | زمان آخرین به‌روزرسانی حساب کاربری. |

---

### ۳. موجودیت اعضای سازمان (`organization_members`)

* **وظیفه**: جدول واسط چندبه‌چند (M:N) برای اتصال کاربران به سازمان‌ها به همراه نقش و سطح دسترسی کاربر در هر سازمان.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف آبشاری CASCADE). |
| `user_id` | `UUID` | کلید خارجی به جدول `users.id` (اجباری، حذف آبشاری CASCADE). |
| `role` | `VARCHAR(50)` | نقش کاربر در سازمان (`admin`، `member`، `viewer` - در V1: `admin`). |
| `status` | `VARCHAR(20)` | وضعیت عضویت (`active`، `invited`، `suspended` - پیش‌فرض: `active`). |
| `joined_at` | `TIMESTAMPTZ` | زمان پیوستن کاربر به سازمان. |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد رکورد. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی رکورد. |

* **قیدهای یکتایی**: زوج مرتب `(organization_id, user_id)` یکتا است.

---

### ۴. موجودیت منابع (`resources`)

* **وظیفه**: تعریف منبع یا سرور هدف تحت مدیریت پشتیبان‌گیری (مانند سرور اوبونتو یا اکانت cPanel).
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `name` | `VARCHAR(100)` | نام یا برچسب کاربرپسند برای منبع (مثال: "Ubuntu Main DB Server"). |
| `type` | `VARCHAR(50)` | نوع منبع (`ubuntu_ssh`، `cpanel`). |
| `status` | `VARCHAR(30)` | وضعیت عملیاتی منبع (`active`، `unreachable`، `disabled`، `error`، `archived`). |
| `last_connection_test_at` | `TIMESTAMPTZ` | زمان آخرین اجرای موفق یا ناموفق تست اتصال (قابل تهی). |
| `last_connection_status` | `VARCHAR(30)` | نتیجه آخرین تست اتصال (`success`، `failed` - قابل تهی). |
| `last_connection_error` | `TEXT` | پیام خطای آخرین تست اتصال به صورت پاک‌سازی‌شده بدون افشای Secret (قابل تهی). |
| `metadata` | `JSONB` | اطلاعات عمومی غیرحساس شناسایی‌شده (مانند نسخه سیستم‌عامل، نسخه MySQL، مسیرها). |
| `created_at` | `TIMESTAMPTZ` | زمان ثبت منبع. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی منبع. |

---

### ۵. موجودیت اتصال‌دهنده منبع (`resource_connectors`)

* **وظیفه**: نگهداری تنظیمات شبکه، پروتکل ارتباطی و پارامترهای غیرحساس مورد نیاز کانکتور جهت اتصال به منبع، و انتساب به اطلاعات هویتی امن.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).
* **تضمین عدم ارجاع بین‌سازمانی**: مقدار `organization_id` در این جدول **باید همواره با `organization_id` موجودیت `resources` ارجاع‌شده (و همچنین `credentials` متصل) کاملاً یکسان باشد**.

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (تضمین‌کننده ایزولاسیون سازمانی). |
| `resource_id` | `UUID` | کلید خارجی یکتا به `resources.id` (اجباری، یکتایی 1:1، حذف CASCADE). |
| `connector_type` | `VARCHAR(50)` | نوع کانکتور (`ubuntu_ssh`، `cpanel`). |
| `credential_id` | `UUID` | کلید خارجی به جدول `credentials.id` (ارجاع به سکرت رمزشده، حذف RESTRICT). |
| `host` | `VARCHAR(255)` | آدرس IP یا نام میزبان (Hostname) سرور یا هاست مقصد. |
| `port` | `INTEGER` | پورت اتصال (مثال: ۲۲ برای SSH، ۲۰۸۳ برای cPanel HTTPS). |
| `auth_type` | `VARCHAR(50)` | روش اعتبارسنجی (`ssh_key` [پیش‌فرض/ترجیحی]، `ssh_password`، `cpanel_api_token` [پیش‌فرض/ترجیحی]، `cpanel_password`). |
| `host_key_fingerprint` | `VARCHAR(255)` | اثر انگشت کلید میزبان SSH سرور مقصد (Host Key Fingerprint) برای اعتبارسنجی هویت سرور و پیشگیری از MITM (اختیاری - قابل تهی). این فیلد مربوط به سرور مقصد بوده و با `credentials.fingerprint` متفاوت است. |
| `config` | `JSONB` | تنظیمات غیرحساس اضافی ساختاریافته (مانند نام کاربری SSH/cPanel، تنظیمات Timeout، مسیرهای پیش‌فرض). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد رکورد. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی رکورد. |

---

### ۶. موجودیت اطلاعات هویتی و دسترسی‌ها (`credentials`)

* **وظیفه**: صندوق نگهداری امن اطلاعات هویتی و دسترسی‌های حساس (کلید خصوصی SSH، پسوردها، توکن‌های cPanel) به صورت کاملاً رمزنگاری‌شده در حالت سکون.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `name` | `VARCHAR(100)` | نام شناسایی کاربرپسند (مثال: "Ubuntu Root SSH Key", "cPanel Backup API Token"). |
| `type` | `VARCHAR(50)` | نوع داده حساس (در نسخه ۱: `ssh_private_key`، `ssh_password`، `cpanel_api_token`، `cpanel_password`). این لیست قابل توسعه بوده و در آینده دسترسی‌های فضاهای ذخیره‌سازی مانند S3 (نظیر `s3_access_key`) نیز می‌توانند بدون تغییر در معماری اصلی سیستم به آن افزوده شوند. |
| `encrypted_secret` | `BYTEA` | محتوای رمزگذاری‌شده سکرت با استاندارد AES-256-GCM (هرگز متن خام ذخیره نمی‌شود). این فیلد می‌تواند یک Secure Credential Payload ساختاریافته رمزگذاری‌شده باشد؛ بنابراین در صورت نیاز، چند مقدار مرتبط با یک Credential (مانند SSH Private Key به همراه Passphrase مربوطه) به صورت یک Payload واحد و رمزگذاری‌شده درون آن نگهداری می‌شوند. |
| `nonce` | `BYTEA` | بردار مقداردهی اولیه تصادفی و منحصر‌به‌فرد (Initialization Vector) برای الگوریتم GCM. |
| `auth_tag` | `BYTEA` | تگ احراز اصالت رمزنگاری (GCM Authentication Tag) جهت اطمینان از عدم دستکاری. |
| `key_version` | `INTEGER` | نسخه کلید اصلی رمزنگاری (Master Key Version) جهت پشتیبانی از چرخش دوره‌ای کلیدها (Key Rotation). |
| `fingerprint` | `VARCHAR(255)` | اثر انگشت عمومی یا هش شناسایی کلید جهت نمایش به کاربر بدون نیاز به رمزگشایی سکرت (قابل تهی). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد رکورد. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی رکورد. |

---

## ۴. موجودیت‌های برنامه‌ریزی، اجرا و خروجی‌های پشتیبان (Plans, Jobs, Runs & Artifacts)

### ۷. موجودیت برنامه‌های پشتیبان‌گیری (`backup_plans`)

* **وظیفه**: تعریف و ذخیره‌سازی تنظیمات پشتیبان‌گیری برای یک Resource مشخص شامل انتخاب اهداف (دیتابیس‌ها و فایل‌ها)، الگوی زمان‌بندی (Schedule Cron) و سیاست نگهداری (Retention Policy).
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `resource_id` | `UUID` | کلید خارجی به جدول `resources.id` (اجباری، حذف CASCADE). |
| `name` | `VARCHAR(100)` | نام برنامه پشتیبان‌گیری (مثال: "Daily MySQL Full Backup"). |
| `backup_type` | `VARCHAR(50)` | نوع بکاپ هدف (`mysql_database`، `website_files`، `both`). |
| `target_spec` | `JSONB` | لیست اهداف انتخابی (مانند `{"databases": ["db_prod", "db_analytics"], "paths": ["/var/www/site"]}`). |
| `schedule_cron` | `VARCHAR(100)` | الگوی استاندارد زمان‌بندی Cron (مثال: `0 2 * * *` برای ساعت ۲ بامداد هر روز - در صورت عدم زمان‌بندی قابل تهی). |
| `schedule_timezone` | `VARCHAR(50)` | منطقه زمانی اجرای الگوی زمان‌بندی Cron (پیش‌فرض: `'UTC'`، مثال: `'Asia/Tehran'`، `'UTC'`) جهت تعیین دقیق زمان محلی اجرای بکاپ‌ها و آمادگی کامل برای مدل چندمستأجری/SaaS. |
| `is_schedule_enabled` | `BOOLEAN` | وضعیت فعال بودن زمان‌بند خودکار (پیش‌فرض: true). |
| `retention_count` | `INTEGER` | حداکثر تعداد نسخه‌های موفق برای نگهداری (مثال: 7 یا 30 - قابل تهی). |
| `retention_days` | `INTEGER` | حداکثر تعداد روزهای نگهداری نسخه‌های قدیمی (قابل تهی). |
| `status` | `VARCHAR(20)` | وضعیت برنامه (`active`، `paused`، `archived` - پیش‌فرض: `active`). |
| `next_run_at` | `TIMESTAMPTZ` | زمان محاسبه‌شده برای اجرای نوبت بعدی توسط زمان‌بند (قابل تهی). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد برنامه. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی برنامه. |

* **قیدهای اعتبارسنجی و سازمانی**:
  * مقدار `organization_id` در `backup_plans` باید با `organization_id` مربوط به `resources` ارجاع‌شده یکسان باشد.
  * **قید صحت زمان‌بندی**: در صورتی که `is_schedule_enabled = true` باشد، فیلد `schedule_cron` الزامی است و باید شامل یک عبارت زمان‌بندی استاندارد و معتبر باشد.
* **روابط**:
  * متعلق به `resources` (رابطه چند به یک).
  * یک به چند (1:N) با `backup_jobs`.

---

### ۸. موجودیت جاب‌های پشتیبان‌گیری (`backup_jobs`)

* **وظیفه**: ثبت درخواست منطقی اجرای بکاپ. این درخواست می‌تواند توسط موتور زمان‌بند بر اساس یک `BackupPlan` تولید شود یا مستقیماً توسط مدیر به صورت دستی (`Manual Job`) ثبت گردد.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `resource_id` | `UUID` | کلید خارجی به جدول `resources.id` (اجباری، حذف RESTRICT). |
| `backup_plan_id` | `UUID` | کلید خارجی به `backup_plans.id` (قابل تهی - برای جاب‌های دستی اختیاری است، حذف SET NULL). |
| `trigger_type` | `VARCHAR(30)` | منشأ ایجاد جاب (`scheduled` ناشی از زمان‌بند، `manual` درخواست دستی مدیر). |
| `created_by_user_id` | `UUID` | کلید خارجی به `users.id` (شناسه کاربری که جاب دستی را ایجاد کرده؛ برای جاب‌های سیستمی/زمان‌بند قابل تهی). |
| `backup_type` | `VARCHAR(50)` | نوع بکاپ درخواستی (`mysql_database`، `website_files`، `both`). |
| `target_spec` | `JSONB` | اسنپ‌شات قطعی از اهداف مشخص‌شده در لحظه ایجاد جاب (عدم تغییر در صورت ویرایش بعدی پلن). |
| `status` | `VARCHAR(30)` | وضعیت منطقی کلی جاب (`pending`، `running`، `completed`، `failed`، `cancelled` - پیش‌فرض: `pending`). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد درخواست جاب. |
| `updated_at` | `TIMESTAMPTZ` | زمان آخرین تغییر وضعیت جاب. |

* **قیدهای سازمانی**: تطابق اجباری `organization_id` میان `backup_jobs` و `resources` (و `backup_plans` در صورت انتساب).
* **روابط**:
  * متعلق به `resources`.
  * وابستگی اختیاری به `backup_plans` (جاب دستی می‌تواند بدون پلن ایجاد شود).
  * یک به چند (1:N) با `backup_runs` (جهت پشتیبانی از تلاش‌های مجدد / Retry).

---

### ۹. موجودیت تلاش‌های اجرایی جاب (`backup_runs`)

* **وظیفه**: ثبت یک تلاش اجرایی واقعی و مشخص برای انجام یک `BackupJob`. در صورتی که اجرای یک جاب با شکست مواجه شده و مکانیزم Retry فعال شود، جاب دارای چندین Run خواهد بود. این موجودیت کلیه فیلدهای لازم جهت Crash Recovery و مدیریت قفل/مهلت اجرا را داراست.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `job_id` | `UUID` | کلید خارجی به جدول `backup_jobs.id` (اجباری، حذف RESTRICT جهت حفظ تاریخچه). |
| `attempt_number` | `INTEGER` | شماره تلاش اجرایی برای این جاب (شروع از ۱، ۲، ...). |
| `status` | `VARCHAR(30)` | وضعیت این تلاش (`pending`، `running`، `success`، `failed`، `cancelled` - پیش‌فرض: `pending`). |
| `started_at` | `TIMESTAMPTZ` | زمان دقیق شروع پردازش توسط کارگر (Worker). |
| `ended_at` | `TIMESTAMPTZ` | زمان دقیق خاتمه پردازش (موفق یا ناموفق - در حین اجرا قابل تهی). |
| `heartbeat_at` | `TIMESTAMPTZ` | زمان آخرین ضربان حیات ارسالی توسط کارگر در طول استریم داده جهت تشخیص زنده بودن پروسه. |
| `lease_until` | `TIMESTAMPTZ` | مهلت انقضای اجاره اجرای کارگر؛ در صورت عدم تمدید و گذشتن از این زمان، Run به عنوان Crash/Stale شناسایی می‌شود. |
| `error_message` | `TEXT` | شرح خطای عملیاتی پاک‌سازی‌شده در صورت بروز شکست (Sanitized Error - بدون افشای رمزها یا دسترسی‌ها). |
| `logs_summary` | `JSONB` | خلاصه مراحل گام‌به‌گام اجرایی و رخدادهای کلیدی تلاش جاری. |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد رکورد تلاش اجرایی. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی وضعیت تلاش. |

* **حفظ تاریخچه و جلوگیری از حذف آبشاری**: کلید خارجی `job_id` دارای قید `ON DELETE RESTRICT` است تا از حذف تصادفی یا ناخواسته تاریخچه جاب‌ها و تلاش‌های اجرایی جلوگیری شود؛ پاک‌سازی داده‌های تاریخی صرفاً از طریق فرآیندهای کنترل‌شده انجام می‌پذیرد.
* **پشتیبانی از Stale Job & Crash Recovery**:
  * در زمان Startup یا توسط بررسی‌کننده دوره‌ای، هر رکوردی با `status = 'running'` که مقدار `lease_until < NOW()` یا `heartbeat_at` منقضی‌شده داشته باشد، به وضعیت `FAILED` منتقل شده و بر اساس سیاست سیستم تصمیم‌گیری برای ایجاد Run جدید (Retry) یا خاتمه Job اتخاذ می‌شود.
* **روابط**:
  * متعلق به `backup_jobs`.
  * یک به چند (1:N) با `backup_artifacts`.

---

### ۱۰. موجودیت آرتیفکت‌های خروجی بکاپ (`backup_artifacts`)

* **وظیفه**: ثبت مشخصات، ابرداده، شناسه مقصد ذخیره‌سازی و شناسه ذخیره‌سازی فایل‌های خروجی واقعی تولیدشده توسط یک Run موفق (نظیر فایل خروجی دیتابیس `.sql.gz` یا آرشیو فایل‌های وب‌سایت `.tar.gz`). یک Run می‌تواند در صورت پشتیبان‌گیری از چند دیتابیس یا فایل، چند Artifact تولید کند.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `run_id` | `UUID` | کلید خارجی به جدول `backup_runs.id` (اجباری، حذف RESTRICT جهت جلوگیری از حذف ناخواسته فایل‌ها). |
| `resource_id` | `UUID` | کلید خارجی به جدول `resources.id` (جهت دسترسی و فیلتر سریع بر اساس منبع). |
| `storage_target_id` | `UUID` | کلید خارجی به جدول `storage_targets.id` (اجباری، حذف RESTRICT). مشخص‌کننده مقصد ذخیره‌سازی این آرتیفکت. |
| `artifact_type` | `VARCHAR(50)` | نوع آرتیفکت خروجی (`database_dump`، `files_archive`). |
| `format` | `VARCHAR(30)` | فرمت بسته‌بندی فایل (`sql_gzip`، `tar_gzip`). |
| `target_name` | `VARCHAR(255)` | نام موجودیت پشتیبان‌گیری‌شده (مثال: نام دیتابیس "app_db" یا شناسه مسیر "public_html"). |
| `storage_reference` | `VARCHAR(500)` | شناسه و ارجاع داخلی ذخیره‌سازی (مثال: `organizations/{org_id}/resources/{res_id}/artifacts/{artifact_id}.sql.gz`). این مقدار یک شناسه انتزاعی داخلی است و هرگز مسیر مطلق سرور به کاربر افشا نمی‌شود. |
| `size_bytes` | `BIGINT` | حجم دقیق فایل خروجی بر حسب بایت. |
| `checksum_algorithm` | `VARCHAR(30)` | الگوریتم محاسبه هش یکپارچگی (پیش‌فرض: `sha256`). |
| `checksum_hash` | `VARCHAR(128)` | مقدار هش هگزادسیمال محاسبه‌شده هم‌زمان با استریم فایل جهت بررسی عدم دستکاری و خرابی. |
| `verification_status` | `VARCHAR(30)` | وضعیت اعتبارسنجی سلامت فایل (`unverified`، `verified`، `failed` - پیش‌فرض: `unverified`). |
| `verified_at` | `TIMESTAMPTZ` | زمان انجام عملیات بررسی صحت و اعتبارسنجی فایل (قابل تهی). |
| `verification_details` | `TEXT` | جزئیات نتیجه تست اعتبارسنجی و خوانایی فایل پشتیبان. |
| `is_deleted` | `BOOLEAN` | نشان‌دهنده حذف فیزیکی فایل بر اساس سیاست نگهداری یا حذف توسط کاربر (پیش‌فرض: false). |
| `deleted_at` | `TIMESTAMPTZ` | زمان حذف فایل از دیسک (قابل تهی). |
| `created_at` | `TIMESTAMPTZ` | زمان تولید و ذخیره کامل آرتیفکت. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی رکورد. |

* **حفظ تاریخچه و کنترل دقیق حذف آرتیفکت‌ها**: کلید خارجی `run_id` دارای قید `ON DELETE RESTRICT` است تا متادیتا و فایل‌های فیزیکی بکاپ با حذف تصادفی یا ناخواسته رکورد Run یا Job از بین نروند؛ عملیات حذف آرتیفکت صرفاً بر اساس سیاست‌های نگهداری (Retention Policy) یا فرآیند کنترل‌شده حذف مجاز (`Authorized Backup Delete`) توسط ادمین مجاز صورت می‌گیرد.
* **تضمین سازگاری شناسه منبع در زنجیره اجرا**: مقدار `backup_artifacts.resource_id` باید همواره و بدون استثنا با منبع (`Resource`) مربوط به زنجیره `BackupArtifact → BackupRun → BackupJob → Resource` یکسان باشد و این سازگاری در لایه دسترسی به داده (Data Layer) تضمین می‌شود.
* **قیدهای سازمانی**: تضمین تطابق کامل `organization_id` میان `backup_artifacts`، `backup_runs`، `resources` و `storage_targets`.
* **روابط**:
  * متعلق به `backup_runs`.
  * منتسب به `resources`.
  * ذخیره‌شده در `storage_targets`.

---

## ۵. موجودیت‌های مقاصد ذخیره‌سازی و لاگ‌های حسابرسی (Storage Targets & Audit Logs)

### ۱۱. موجودیت مقصدهای ذخیره‌سازی (`storage_targets`)

* **وظیفه**: تعریف مقصد فیزیکی یا ابری ذخیره‌سازی فایل‌های پشتیبان. در نسخه ۱ منحصراً نوع `local` (مسیر ایزوله خارج از وب‌روت `/srv/backup-platform/`) پشتیبانی می‌شود، اما ساختار مدل برای پشتیبانی از ذخیره‌سازهای سازگار با S3 و فضاهای ریموت در آینده کاملاً مهیا است.
* **دامنه‌بندی سازمانی**: دارد (`organization_id`).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (اجباری، حذف RESTRICT). |
| `name` | `VARCHAR(100)` | نام یا برچسب کاربرپسند مقصد ذخیره‌سازی (مثال: "Default Local Storage", "Wasabi Primary Bucket"). |
| `type` | `VARCHAR(50)` | نوع مقصد ذخیره‌سازی (`local` در نسخه ۱؛ `s3`، `s3_compatible`، `remote_ssh` در فازهای آتی). |
| `status` | `VARCHAR(20)` | وضعیت عملیاتی مقصد (`active`، `disabled`، `error` - پیش‌فرض: `active`). |
| `is_default` | `BOOLEAN` | مشخص‌کننده استوریج پیش‌فرض سازمان برای آرتیفکت‌های جدید (پیش‌فرض: false). در هر Organization حداکثر یک StorageTarget می‌تواند مقدار `is_default = true` داشته باشد (اعمال از طریق Partial Unique Index / Constraint). |
| `credential_id` | `UUID` | کلید خارجی اختیاری به جدول `credentials.id` (جهت نگهداری دسترسی‌های ابری مانند S3 Access/Secret Key در آینده بدون ذخیره متن خام، حذف RESTRICT). |
| `config` | `JSONB` | تنظیمات غیرحساس مقصد ذخیره‌سازی (مانند مسیر پایه `/srv/backup-platform` برای Local، یا Bucket/Region/Endpoint برای S3 در آینده). |
| `created_at` | `TIMESTAMPTZ` | زمان تعریف مقصد ذخیره‌سازی. |
| `updated_at` | `TIMESTAMPTZ` | زمان به‌روزرسانی تنظیمات مقصد. |

* **قیدهای سازمانی و یکتایی**:
  * در هر سازمان منحصراً حداکثر یک رکورد با `is_default = true` مجاز است.
  * در صورت انتساب `credential_id`، مقدار `organization_id` در `storage_targets` باید با `organization_id` مربوط به `credentials` یکسان باشد.
* **روابط**:
  * متعلق به `organizations`.
  * ارتباط اختیاری با `credentials` (برای دسترسی‌های ابری آینده).
  * یک به چند (1:N) با `backup_artifacts`.

---

### ۱۲. موجودیت لاگ‌های حسابرسی و امنیتی (`audit_logs`)

* **وظیفه**: ثبت افزایشی (Append-Oriented) و مطمئن تمامی رویدادها، تغییرات و عملیات حساس و امنیتی سیستم با حفظ محرمانگی و عدم افشای اطلاعات حساس.
* **دامنه‌بندی سازمانی**: دارد (در صورت مرتبط بودن با یک سازمان خاص، مقدار `organization_id` پر می‌شود؛ برای رخدادهای عمومی سیستمی یا تلاش‌های ناموفق لاگین قبل از تشخیص سازمان، قابل تهی است).

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `organization_id` | `UUID` | کلید خارجی به جدول `organizations.id` (قابل تهی برای وقایع عمومی؛ با سیاست حفظ تاریخچه و حذف RESTRICT). |
| `user_id` | `UUID` | کلید خارجی به جدول `users.id` (شناسه کاربر عامل؛ برای رویدادهای خودکار زمان‌بند یا تلاش‌های ناشناس لاگین قابل تهی است، حذف SET NULL). |
| `action` | `VARCHAR(100)` | عنوان رویداد انجام‌شده (مثال: `auth.login.success`، `auth.login.failed`، `auth.logout`، `resource.create`، `credential.update`، `backup.download`، `backup.delete`، `backup_plan.update`، `retention.cleanup`). |
| `entity_type` | `VARCHAR(50)` | نوع موجودیت تحت تأثیر (`user`، `organization`، `resource`، `credential`، `backup_plan`، `backup_job`، `backup_run`، `backup_artifact`، `storage_target`، `system`). |
| `entity_id` | `UUID` | شناسه یکتای موجودیت تغییریافته یا فراخوانی‌شده (قابل تهی). |
| `ip_address` | `VARCHAR(45)` | آدرس IP کلاینت درخواست‌دهنده (پشتیبانی از IPv4 و IPv6). |
| `user_agent` | `VARCHAR(255)` | اطلاعات مرورگر یا کلاینت ارسال‌کننده درخواست (قابل تهی). |
| `metadata` | `JSONB` | جزئیات و بافتار عملیاتی پاک‌سازی‌شده رویداد (مانند نام فیلدهای تغییریافته، حجم فایل دانلودشده، نتیجه تست اتصال). **الزام قطعی: هیچ رمز عبور، کلید خصوصی، توکن API یا سکرت نباید در این فیلد ثبت شود.** |
| `created_at` | `TIMESTAMPTZ` | زمان دقیق ثبت رویداد. |

* **سیاست حفظ تاریخچه و عدم حذف لاگ‌ها**:
  * حذف فیزیکی سازمان‌های دارای تاریخچه Audit Log ممنوع بوده و با قید `ON DELETE RESTRICT` محافظت می‌شود. سازمان‌ها در شرایط عادی غیرفعال‌سازی صرفاً آرشیو (`status = 'archived'`) می‌شوند تا پیوند تاریخچه Audit Logها دست‌نخورده باقی بماند.
* **ماهیت لاگ‌ها**: لاگ‌های حسابرسی به صورت ساختار افزایشی (`append-oriented`) درج می‌شوند. به دلیل عدم وجود زیرساخت سخت‌افزاری/نرم‌افزاری WORM در نسخه ۱، این موجودیت `immutable` یا `WORM` معرفی نمی‌شود ولی در برابر تغییرات عادی کاربران در لایه سرویس محافظت می‌گردد. ثبت لاگ‌های حساس به صورت درج مستقیم و پایدار در تراکنش یا چرخه درخواست وب/سرویس صورت می‌گیرد.

---

### ۱۳. موجودیت نشست‌های کاربری (`user_sessions`)

* **وظیفه**: مدیریت نشست‌های فعال کاربران، ذخیره‌سازی هش توکن‌های رفرش و فراهم‌سازی قابلیت ابطال بلادرنگ نشست‌ها در سمت سرور (`Server-side Revocation`).
* **دامنه‌بندی سازمانی**: ندارد (Global / User-scoped Entity). هویت کاربران سراسری است و نشست‌ها به کاربر انتساب دارند.

| نام فیلد | نوع داده | توضیحات و الزامات |
| :--- | :--- | :--- |
| `id` | `UUID` | کلید اصلی (Primary Key). |
| `user_id` | `UUID` | کلید خارجی به جدول `users.id` (اجباری، حذف آبشاری CASCADE). |
| `refresh_token_hash` | `VARCHAR(64)` | هش یک‌طرفه امن توکن رفرش تصادفی با آنتروپی بالا (SHA-256). **الزام قطعی: توکن خام هرگز در دیتابیس یا لاگ‌ها ذخیره نمی‌شود.** |
| `ip_address` | `VARCHAR(45)` | آدرس IP کلاینت در زمان ایجاد یا آخرین فعالیت نشست (قابل تهی). |
| `user_agent` | `TEXT` | رشته اطلاعات مرورگر/کلاینت برای نمایش نشست‌های فعال به کاربر (قابل تهی). |
| `created_at` | `TIMESTAMPTZ` | زمان ایجاد نشست. |
| `last_used_at` | `TIMESTAMPTZ` | زمان آخرین استفاده جهت تمدید توکن دسترسی. |
| `expires_at` | `TIMESTAMPTZ` | زمان انقضای قطعی نشست (مثلاً ۷ روز). |
| `revoked_at` | `TIMESTAMPTZ` | زمان ابطال دستی نشست در زمان Logout، تغییر پسورد یا غیرفعال‌سازی کاربر (در صورت فعال بودن نشست، مقدار NULL است). |

* **قیدها و شاخص‌های مفهومی**:
  * قید یکتایی و ایندکس یکتا (Unique Index) روی `refresh_token_hash` جهت جستجوی سریع توکن و ممانعت از ایجاد توکن‌های تکراری.
  * ایندکس روی `user_id` جهت واکشی سریع نشست‌های فعال یک کاربر و ابطال دسته‌جمعی در زمان تغییر کلمه عبور یا مسدودسازی.
* **روابط**:
  * متعلق به `users` (رابطه N:1).

---

## ۶. تشریح روابط و قیدهای عدم ارجاع بین‌سازمانی (Cross-Organization Constraints)

برای تضمین ایزولاسیون کامل چندسازمانی (Multi-tenancy) و جلوگیری از هرگونه نشت داده (Data Leakage) میان سازمان‌ها، قواعد یکپارچگی زیر در لایه Data Layer و پایگاه داده اعمال می‌شوند:

1. **یکپارچگی در `resource_connectors`**:
   * `resource_connectors.organization_id == resources.organization_id`
   * `resource_connectors.organization_id == credentials.organization_id`
2. **یکپارچگی در `backup_plans` و `backup_jobs`**:
   * `backup_plans.organization_id == resources.organization_id`
   * `backup_jobs.organization_id == resources.organization_id`
   * در صورت وجود پلن: `backup_jobs.organization_id == backup_plans.organization_id`
3. **یکپارچگی در `backup_runs` و `backup_artifacts`**:
   * `backup_runs.organization_id == backup_jobs.organization_id`
   * `backup_artifacts.organization_id == backup_runs.organization_id`
   * `backup_artifacts.organization_id == storage_targets.organization_id`
   * `backup_artifacts.resource_id == backup_jobs.resource_id` (تطابق کامل در کل زنجیره)
4. **یکپارچگی در `storage_targets`**:
   * در هر سازمان حداکثر یک `StorageTarget` پیش‌فرض مجاز است (`COUNT(is_default = true) <= 1`).
   * در صورت انتساب به Credential ابری: `storage_targets.organization_id == credentials.organization_id`
5. **یکپارچگی در `audit_logs`**:
   * هر لاگ ثبت‌شده با شناسه سازمان، تنها به موجودیت‌ها و کاربران مجاز همان سازمان ارجاع می‌دهد و دسترسی به لاگ‌های سازمان منحصراً برای ادمین همان سازمان یا سوپرادمین سیستم مجاز است. سازمان‌های دارای لاگ حسابرسی به جای حذف فیزیکی، آرشیو می‌شوند.

---

## ۷. نتیجه ارزیابی نهایی مدل داده (Data Model Review Result)

**وضعیت نهایی:** `Approved`

بررسی جامع و تطبیقی مدل داده با اسناد [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md) و [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md) در ۷ محور کلیدی به شرح زیر انجام گرفت و انطباق کامل آن تأیید گردید:

1. **روابط و کلیدهای خارجی (Relationships & Integrity)**:
   * تمامی ارتباطات با Cardinality صریح، Nullable بودن دقیق، و قیدهای Foreign Key مشخص شده‌اند.
   * سیاست‌های `ON DELETE` برای رکوردهای تاریخی و حیاتی (`backup_runs`، `backup_artifacts`، `audit_logs` و `credentials`) از سیاست `RESTRICT` پیروی کرده تا از حذف تصادفی یا آبشاری داده‌ها و فایل‌ها ممانعت به عمل آید.
2. **ایزولاسیون کامل چندسازمانی (Multi-Organization Isolation)**:
   * تمامی موجودیت‌های متعلق به مستأجر دارای ستون `organization_id` هستند.
   * زنجیره‌های `Organization → Resource → Connector → Credential` و `Organization → BackupPlan → BackupJob → BackupRun → BackupArtifact → StorageTarget` با قیدهای صریح در بخش ۶ همگام بوده و امکان Cross-Organization Reference به صفر رسیده است.
3. **یکپارچگی نام‌گذاری (Naming Consistency)**:
   * نام‌گذاری کلیه جداول، ستون‌ها و مفاهیم (`BackupPlan`، `BackupJob`، `BackupRun`، `BackupArtifact`، `StorageTarget`) در تمامی اسناد پروژه کاملاً منطبق، یکپارچه و به صورت استاندارد snake_case تدوین شده است.
4. **پوشش چرخه حیات پشتیبان‌گیری (Backup Lifecycle)**:
   * جریان داده‌ای شامل تعریف پلن، ایجاد درخواست جاب (دستی یا زمان‌بندی‌شده)، اختصاص تلاش اجرایی، تولید فایل خروجی، نگهداری در مقصد ذخیره‌سازی، اعتبارسنجی سلامت آرتیفکت و اعمال سیاست‌های نگهداری/حذف به صورت کامل مدل‌سازی شده است.
5. **تاب‌آوری و بازیابی پس از خرابی (Crash Recovery)**:
   * تعبیه فیلدهای `heartbeat_at`، `lease_until`، `started_at` و `error_message` در موجودیت `backup_runs` قابلیت کشف خودکار جاب‌های معلق/کِرَش‌شده را فراهم ساخته و امکان اجرای مجدد (Retry) بدون بازنویسی یا از بین رفتن سوابق قبلی وجود دارد.
6. **آمادگی برای مدل SaaS و توسعه‌پذیری (SaaS Readiness)**:
   * جداسازی کاربر سراسری از سازمان از طریق جدول `organization_members`، پشتیبانی از منطقه زمانی در زمان‌بندی، فیلدهای `metadata` برای مدیریت سهمیه فضا/تعداد منابع و سطوح اشتراک، و ساختار منعطف `StorageTarget` جهت افزودن S3 در آینده بدون نیاز به بازطراحی معماری تضمین شده است.
7. **الزامات امنیتی و محرمانگی (Security & Privacy)**:
   * رمزنگاری اطلاعات حساس با AES-256-GCM، پنهان‌سازی مسیر فیزیکی فایل‌ها (`storage_reference`)، ممانعت قطعی از ذخیره سکرت‌ها در لاگ‌های حسابرسی، و بهره‌گیری از شناسه‌های غیرقابل حدس UUID نسخه ۴ به صورت استاندارد پیاده‌سازی شده است.
