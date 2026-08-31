# سند معماری سیستم (System Architecture Document)

این سند معماری فنی، ساختار ماژولار، جریان داده‌ها و طراحی لایه‌ای پلتفرم مدیریت پشتیبان‌گیری را بر اساس نیازمندی‌های ثبت‌شده در [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md) تشریح می‌کند.

---

## ۱. معماری سطح بالا (High-Level Architecture)

پلتفرم به صورت یک **Modular Monolith** با زبان **Go** طراحی شده است. تمام زیرسیستم‌ها در قالب یک نرم‌افزار واحد اما با مرزبندی‌های دامنه‌ای (Domain Boundaries) مشخص و مستقل پیاده‌سازی می‌شوند تا علاوه بر سادگی استقرار و مصرف بهینه منابع در نسخه ۱، مسیر جداسازی یا توزیع پردازش‌ها در آینده هموار باشد.

### اصول کلیدی معماری:
* **استقلال هسته از پروتکل‌های ارتباطی**: هسته مرکزی برنامه (`Core Engine`) هیچ‌گونه وابستگی به پروتکل‌های خاص منابع (مانند دستورات مستقیم SSH یا فراخوانی‌های cPanel) ندارد و تنها با لایه انتزاعی کانکتورها (`Connectors`) در تعامل است.
* **تفکیک موتور بکاپ از مقصد ذخیره‌سازی**: موتور بکاپ (`BackupEngine`) مسئول نحوه استخراج و بسته‌بندی داده‌ها است، در حالی که لایه ذخیره‌سازی (`StorageProvider`) صرفاً مقصد نگهداری آرتیفکت‌ها را مدیریت می‌کند.
* **انتزاع کامل لایه ذخیره‌سازی**: عملیات نوشتن، خواندن و حذف فایل‌های بکاپ از طریق اینترفیس `StorageProvider` انجام می‌پذیرد.
* **طراحی ذاتاً چندسازمانی (Multi-Organization Ready)**:
  * داده‌ها و موجودیت‌های متعلق به مستأجر (Tenant-Owned) نظیر `Resource`، `BackupPlan`، `BackupJob`، `BackupRun`، `BackupArtifact` و `StorageTarget` دارای فیلد `organization_id` (Organization-scoped) هستند.
  * موجودیت‌های کاربری (`users`) از طریق جدول انتساب اعضا (`organization_members`) به یک یا چند سازمان متصل می‌شوند.
  * در نسخه ۱، یک سازمان داخلی پیش‌فرض (`Internal Organization`) وجود دارد و ادمین می‌تواند سازمان‌های جدید ایجاد و مدیریت کند.

### ساختار کلی سیستم (ASCII Diagram):

```text
+-----------------------------------------------------------------------------------+
|                                 ADMIN USER / API                                  |
+-----------------------------------------+-----------------------------------------+
                                          | (HTTP / JSON API)
                                          v
+-----------------------------------------------------------------------------------+
|                             MODULAR MONOLITH (GO)                                 |
|                                                                                   |
|  +------------------------+  +--------------------------+  +-------------------+  |
|  | Auth & Security Module |  | Organization Management  |  | Resource Manager  |  |
|  +------------------------+  +--------------------------+  +-------------------+  |
|                                                                                   |
|  +------------------------+  +--------------------------+  +-------------------+  |
|  |    Scheduler Module    |  |     Job & Run Engine     |  | Discovery Module  |  |
|  +------------------------+  +--------------------------+  +-------------------+  |
|                                          |                                        |
|                          (Claim PENDING Jobs from DB)                             |
|                                          v                                        |
|  +-----------------------------------------------------------------------------+  |
|  |             IN-PROCESS WORKER POOL (Goroutines / DB-Claimed)                |  |
|  +-----------------------------------------------------------------------------+  |
|          |                               |                                |       |
|          v                               v                                v       |
|  +--------------------+      +-----------------------+      +------------------+  |
|  |     CONNECTOR      |      |     BACKUP ENGINE     |      |     STORAGE      |  |
|  |    ABSTRACTION     |      |      ABSTRACTION      |      |   ABSTRACTION    |  |
|  |  +---------------+ |      |  +------------------+ |      |  +-------------+ |  |
|  |  | SSH Connector | | <--> |  | Direct Streaming | | ---> |  | LocalStorage| |  |
|  |  | (Ubuntu/Root) | |      |  | (Future: restic) | |      |  | Provider    | |  |
|  |  +---------------+ |      |  +------------------+ |      |  | (/srv/...)  | |  |
|  |  | cPanel Conn   | |      +-----------------------+      |  +-------------+ |  |
|  |  | (API Token)   | |                                     |  | (Future: S3)| |  |
|  |  +---------------+ |                                     |  +-------------+ |  |
|  +--------------------+                                     +------------------+  |
+-----------------------------------------------------------------------------------+
       |                                                               |
       | (Remote Execution / API)                          (File I/O outside WebRoot)
       v                                                               v
+-------------------------------+                             +---------------------+
|       TARGET RESOURCES        |                             | LOCAL STORAGE       |
|  +-------------------------+  |                             | /srv/backup-platform|
|  | Ubuntu Server (SSH/Root)|  |                             +---------------------+
|  +-------------------------+  |                                        |
|  | Shared Host (cPanel)    |  |                                        v
|  +-------------------------+  |                             +---------------------+
+-------------------------------+                             | POSTGRESQL METADATA |
                                                              | Jobs, Runs, Plans   |
                                                              +---------------------+
```

---

## ۲. ماژول‌های هسته (Core Modules)

سیستم به ماژول‌های مجزا با وظایف مشخص تفکیک شده است:

1. **ماژول احراز هویت و امنیت (Auth & Security Module)**:
   * مدیریت نشست‌های پایدار در جدول `user_sessions`، چرخش توکن‌های رفرش و ابطال بلادرنگ سمت سرور.
   * ارتباط کاربران سراسری با سازمان‌ها از طریق `organization_members`.
   * رمزنگاری و رمزگشایی امن دسترسی‌ها (Credentials و API Tokens) در سطح پایگاه داده با استفاده از الگوریتم‌های متقارن استاندارد AES-256-GCM.

2. **ماژول مدیریت سازمان‌ها (Organization Management Module)**:
   * ایجاد Organization داخلی پیش‌فرض و اولین کاربر با دسترسی سوپرادمین (`is_system_admin = true`) در فرآیند راه‌اندازی اولیه (Bootstrap).
   * محدودسازی ایجاد سازمان‌های جدید در نسخه ۱ منحصراً به سوپرادمین سیستم.
   * اعمال فیلتر سازمانی بر تمام موجودیت‌های Tenant-owned.

3. **ماژول مدیریت منابع (Resource Management Module)**:
   * ثبت و مدیریت موجودیت‌های هدف (`ubuntu_ssh` و `cpanel`).
   * نگهداری تنظیمات اتصال و ارتباط با کانکتورها جهت تست اتصال (`Test Connection`).

4. **ماژول کشف خودکار (Discovery Module)**:
   * تعامل با قابلیت کشف کانکتور برای شناسایی خودکار دیتابیس‌های MySQL موجود روی منبع.

5. **ماژول برنامه‌ها و برنامه‌ریزی بکاپ (Backup Plans Module)**:
   * مدیریت `BackupPlan`ها شامل نوع بکاپ (`mysql_database`، `website_files`، `both`)، اهداف انتخابی، الگوی زمان‌بندی و سیاست‌های نگهداری (Retention).

6. **ماژول هسته جاب و تلاش‌های اجرایی (Job & Run Engine Module)**:
   * **تفکیک سه مفهوم کلیدی**:
     * **`BackupJob`**: درخواست منطقی انجام بکاپ (ثبت‌شده به صورت دستی یا ناشی از زمان‌بند BackupPlan). وضعیت‌های کانونی: `pending`, `running`, `completed`, `failed`, `cancelled`.
     * **`BackupRun`**: یک تلاش اجرایی واقعی برای جاب (شامل لاگ‌ها، زمان آغاز و پایان، وضعیت تلاش، مدیریت تلاش مجدد). وضعیت‌های کانونی: `pending`, `running`, `success`, `failed`, `cancelled`.
     * **`BackupArtifact`**: فایل خروجی، متادیتا، سایز و چک‌سام نهایی حاصل از اجرای موفق.

7. **ماژول موتور بکاپ (Backup Engine Module)**:
   * انتزاع منطق استخراج و انتقال داده.
   * در نسخه ۱: موتور استریم مستقیم (Direct Streaming Dump / Archive).
   * در فازهای آتی: موتور داخلی `restic` به عنوان یک موتور پیشرفته برای قابلیت‌های Deduplication و Snapshotting.

8. **ماژول زمان‌بندی (Scheduler Module)**:
   * بررسی دوره‌ای `backup_plans` و ایجاد رکوردهای پایدار `BackupJob` در وضعیت `pending` در دیتابیس PostgreSQL.

9. **ماژول ذخیره‌سازی و نگهداری (Storage & Retention Module)**:
   * مدیریت فایل‌ها در مقصد ذخیره‌سازی از طریق `StorageProvider`.
   * اعمال سیاست نگهداری (`Retention Policy`)، حذف بایت‌های فیزیکی از دیسک و ثبت وضعیت Tombstone (`is_deleted = true`, `deleted_at = timestamp`) در متادیتای آرتیفکت.
   * ارائه دانلود کنترل‌شده و مجاز (`Authorized Backup Download`).

10. **ماژول اعتبارسنجی (Verification Module)**:
    * اعتبارسنجی ساختار فایل و تطابق Checksum آرتیفکت‌های تولیدشده پس از پایان هر Run.

11. **ماژول لاگ‌های عملیاتی و مانیتورینگ (Safe Audit & Logging Module)**:
    * ثبت وقایع اجرایی و خطاها با پاک‌سازی خودکار داده‌های حساس (رمزها، کلیدها، توکن‌ها).

---

## ۳. معماری کانکتورها و تفکیک قابلیت‌ها (Connector Architecture)

برای انعطاف‌پذیری حداکثری و امکان اضافه شدن کانکتورهای متنوع در آینده، ساختار کانکتورها بر پایه تفکیک قابلیت‌ها (Capability-based Interface Design) بنا شده است. هیچ کانکتوری مجبور به پیاده‌سازی هم‌زمان بکاپ فایل و دیتابیس نیست.

### ساختار اینترفیس‌های تفکیک‌شده:

```text
// اینترفیس پایه برای تمامی کانکتورها
BaseConnector:
  - TestConnection(ctx) error
  - Close() error

// قابلیت کشف دیتابیس (اختیاری)
DatabaseDiscoverer:
  - DiscoverDatabases(ctx) ([]DatabaseInfo, error)

// قابلیت بکاپ‌گیری دیتابیس (اختیاری)
DatabaseBackupCapability:
  - BackupDatabase(ctx, dbName, writer io.Writer) error

// قابلیت بکاپ‌گیری فایل‌ها (اختیاری)
FileBackupCapability:
  - BackupFiles(ctx, fileConfig, writer io.Writer) error
```

### ۱. کانکتور لینوکس اوبونتو (SSH Connector):
* **نوع اتصال**: اتصال امن SSH با سطح دسترسی root.
* **پیاده‌سازی قابلیت‌ها**:
  * پیاده‌سازی `DatabaseDiscoverer` و `DatabaseBackupCapability` از طریق اجرای راه دور `mysqldump` یا ابزار معادل و استریم خروجی.
  * پیاده‌سازی `FileBackupCapability` از طریق ایجاد آرشیو فشرده و انتقال استریم.

### ۲. کانکتور هاست اشتراکی (cPanel Connector):
* **نوع اتصال**: ارتباط HTTPS با APIهای cPanel (UAPI) با ترجیح و اولویت استفاده از **API Token** نسبت به پسورد کاربر.
* **پیاده‌سازی قابلیت‌ها**:
  * پیاده‌سازی `DatabaseDiscoverer` و `DatabaseBackupCapability` از طریق APIهای مدیریت دیتابیس cPanel.
  * پیاده‌سازی `FileBackupCapability` از طریق APIهای بکاپ یا فایل‌منیجر cPanel.

---

## ۴. معماری ذخیره‌سازی و موتورهای بکاپ (Storage & Backup Engine Architecture)

تفکیک دقیق دو مفهوم **Backup Engine** (تولیدکننده محتوا) و **Storage Provider** (مقصد ذخیره‌سازی):

### ۱. لایه انتزاعی موتور بکاپ (BackupEngine):
موتور بکاپ وظیفه دارد با همکاری کانکتور، داده‌ها را استخراج و پردازش کند:
* **موتور استریم مستقیم (Direct Stream Engine - نسخه ۱)**: داده‌های خام خروجی دیتابیس یا فایل‌ها را دریافت، به صورت استریم فشرده (مانند gzip) و مستقیماً به StorageProvider تحویل می‌دهد.
* **موتور restic (برنامه آینده)**: به عنوان یک `BackupEngine` داخلی، وظیفه ایجاد اسنپ‌شات‌های رمزگذاری‌شده و بدون تکرار (Deduplicated) را در لایه ذخیره‌سازی مقصد به عهده می‌گیرد؛ بدون اینکه کاربر نهایی با ساختار داخلی restic درگیر شود.

### ۲. لایه انتزاعی مقصد ذخیره‌سازی (StorageProvider):
مسئول مدیریت مقصد فیزیکی ذخیره‌سازی:

```text
StorageProvider Interface:
  - SaveArtifact(ctx, orgID, resourceID, runID, reader io.Reader) (ArtifactMetadata, error)
  - GetArtifactReader(ctx, artifactID) (io.ReadCloser, error)
  - DeleteArtifact(ctx, artifactID) error
  - VerifyIntegrity(ctx, artifactID, expectedChecksum string) (bool, error)
```

### پیاده‌سازی ذخیره‌سازی محلی (LocalStorageProvider):
* **موقعیت فیزیکی**: مسیر امن خارج از وب‌روت سرور: `/srv/backup-platform/`
* **ساختار دایرکتوری‌ها**:
  ```text
  /srv/backup-platform/
  └── organizations/
      └── {organization_id}/
          └── resources/
              └── {resource_id}/
                  └── artifacts/
                      ├── {run_id}_db.sql.gz
                      └── {run_id}_files.tar.gz
  ```
* **امنیت ذخیره‌سازی**: دسترسی دایرکتوری با مجوزهای سخت‌گیرانه سیستم‌عامل (0700)، عدم وجود هرگونه دسترسی وب، دانلود فایل‌ها صرفاً با تایید هویت و لاگینگ امن.

### آمادگی برای S3:
* ساختار `StorageProvider` به‌گونه‌ای است که پیاده‌سازی `S3StorageProvider` (برای فضاهای سازگار با S3) در نسخه‌های بعدی بدون دستکاری هسته به عنوان یک مقصد ذخیره‌سازی مستقل اضافه می‌شود.

---

## ۵. جریان اجرای جاب و تلاش‌های اجرایی (Backup Job & Run Flow)

تمامی فرآیندهای بکاپ دارای تفکیک مشخص بین جاب (Job) و تلاش اجرایی (Run) هستند:

### دیاگرام جریان اجرای جاب (ASCII Diagram):

```text
[ Trigger: Manual Action (Admin/Member) OR Scheduled BackupPlan Event ]
                                |
                                v
+-----------------------------------------------------------------------+
| 1. Job Creation in PostgreSQL                                         |
|    - Insert into `backup_jobs` (Status: pending, org_id, target_spec) |
+-----------------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------------+
| 2. Worker Job Claiming & Run Initialization                           |
|    - Worker finds `pending` job from PostgreSQL queue                 |
|    - Acquire in-process `Per-Resource Mutex`                          |
|    - Transactionally claim Job in PostgreSQL (Job status -> running)  |
|    - Create `backup_runs` record (Status: running, attempt_number)    |
|    - Start heartbeat (`heartbeat_at` & `lease_until` in Run)          |
+-----------------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------------+
| 3. Execution via Connector & BackupEngine                             |
|    - Fetch & decrypt Resource credentials safely in connector scope   |
|    - BackupEngine coordinates with Connector for DB/Files stream      |
|    - Stream data pipe -> Compute SHA-256 Checksum on-the-fly          |
|    - Stream pipe -> StorageProvider writes to /srv/backup-platform/   |
+-----------------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------------+
| 4. Verification & Finalization                                        |
|    - Verification Module validates checksum & archive integrity       |
|    - Insert `backup_artifacts` metadata record                        |
|    - Update `backup_runs` (Status: success, ended_at)                 |
|    - Update `backup_jobs` (Status: completed)                         |
+-----------------------------------------------------------------------+
                                |
                                v
+-----------------------------------------------------------------------+
| 5. Retention Management & Cleanup                                     |
|    - Evaluate Retention Policy (Conservative OR: last N OR < D days)  |
|    - Delete physical artifact bytes & set tombstone metadata          |
|    - Release Per-Resource Mutex                                       |
|    - Write sanitized operational audit log                            |
+-----------------------------------------------------------------------+
```

---

## ۶. مفهوم کارگرها، پایداری جاب‌ها و کنترل همروندی (Worker & Concurrency Concept)

### ۱. پایداری جاب‌ها در پایگاه داده (Durable State in PostgreSQL):
* در نسخه ۱، Worker Pool با استفاده از `Goroutine`ها پیاده‌سازی می‌شود، اما **PostgreSQL تنها منبع پایدار (Durable Source of Truth)** برای جاب‌ها است.
* زمان‌بند یا درخواست‌دهنده جاب، رکورد `BackupJob` را در دیتابیس با وضعیت `pending` درج می‌کند.
* در صورت Restart شدن سرور یا برنامه، هیچ جابی گم نمی‌شود؛ کارگرها جاب‌های `pending` را به صورت پایدار و امن (از طریق تراکنش و تغییر وضعیت به `running`) Claim می‌کنند.

### ۲. استراتژی قفل‌گذاری و جلوگیری از تداخل (Resource Locking Strategy):
* **نسخه ۱ (Local Concurrency Guard)**: کنترل همروندی و ممانعت از اجرای هم‌زمان دو عملیات روی یک منبع منحصراً با استفاده از یک `Per-Resource Mutex` در رم فرآیند Go مدیریت می‌شود.
* **تفکیک با فیلدهای Heartbeat**: فیلدهای `heartbeat_at` و `lease_until` در جدول `backup_runs` به عنوان قفل منبع توزیع‌شده عمل نمی‌کنند؛ بلکه صرفاً برای مالکیت اجرای Run و کشف ران‌های متوقف‌شده (Stale Run / Crash Detection) به کار می‌روند.
* **آمادگی برای مقیاس‌پذیری (Future Distributed Lock/Lease)**: قفل‌گذاری توزیع‌شده با اجاره دیتابیسی (DB-backed Resource Lock / Lease) منحصراً مربوط به معماری چندکارگری آینده (Future Multi-Worker) است.

### ۳. خاموشی ایمن (Graceful Shutdown):
* هنگام دریافت سیگنال توقف (`SIGTERM`/`SIGINT`)، کارگرها فرصت دارند مراحل استریم جاری را نهایی کنند یا وضعیت Run جاری را به درستی به همراه لاگ خطا در دیتابیس ثبت نمایند.

### ۴. بازیابی جاب‌ها و تلاش‌های رهاشده (Stale Job & Run Recovery):
* اگر نرم‌افزار در حین اجرای یک `BackupRun` دچار Crash یا Restart ناگهانی شود، وضعیت آن نباید برای همیشه در حالت `running` مسدود بماند.
* در نسخه ۱، از یک راهکار مبتنی بر PostgreSQL برای مدیریت وضعیت‌های رهاشده (Zombie Reaper) استفاده می‌شود:
  * ثبت زمان آغاز اجرا (`started_at`) برای هر `BackupRun`.
  * تمدید دوره‌ای ضربان حیات و مهلت اجاره (`heartbeat_at` و `lease_until = NOW() + 2m`) توسط کارگر در طول پردازش.
  * شناسایی خودکار Runهای رهاشده در وضعیت `running` در زمان راه‌اندازی مجدد برنامه (`Startup Recovery`) یا توسط تسک دوره‌ای.
  * تغییر وضعیت Runهای رهاشده به `failed` و تصمیم‌گیری برای خاتمه Job یا اجرای مجدد بر اساس سیاست سیستم.
  * سوابق تاریخی `BackupJob` و `BackupRun` برای اهداف ممیزی و حسابرسی همواره حفظ می‌شوند.

---

## ۷. مفهوم زمان‌بندی (Scheduler Concept)

* سرویس زمان‌بند داخلی بر پایه Ticker به طور منظم جدول `backup_plans` را ارزیابی می‌کند.
* برای هر Plan که نوبت اجرای آن فرا رسیده باشد، یک رکورد `BackupJob` در وضعیت `PENDING` در دیتابیس PostgreSQL ثبت می‌نماید تا توسط استخر کارگرها Claim شود.
* اگر برای یک منبع، جاب فعالی در حال اجرا باشد، زمان‌بند بر اساس خط‌مشی تعریف‌شده عمل کرده و از ثبت جاب تکراری یا تداخل جلوگیری می‌نماید.

---

## ۸. بستر و مشخصات استقرار (Deployment Concept)

### مشخصات محیط استقرار اولیه:
* **سیستم‌عامل**: Ubuntu Server 22.04 LTS
* **سخت‌افزار سرور**: 8 هسته CPU، 16 گیگابایت RAM، 100 گیگابایت دیسک.
* **هم‌مکانی (Co-existence)**: میزبانی پلتفرم در کنار ۲ وب‌سایت سبک دیگر؛ مصرف حافظه به واسطه استریم مستقیم بدون بافر کل فایل در RAM بسیار ناچیز و بهینه باقی می‌ماند.
* **سرویس پایگاه داده**: PostgreSQL محلی روی سرور.
* **مسیر ذخیره‌سازی محلی**: دایرکتوری ایزوله `/srv/backup-platform/` با ساختار سازمانی و دسترسی محدود.
* **دسترسی شبکه**: دسترسی مستقیم از طریق IP در فاز اولیه (آماده برای افزودن دامنه‌ها و SSL/TLS در فازهای آتی).

---

## ۹. مقیاس‌پذیری و توسعه‌پذیری آینده (Future Scalability)

معماری پلتفرم برای توسعه به سمت مدل‌های تجاری و توزیع‌شده کاملاً مهیا است:

1. **توزیع کارگرها (Multiple External Workers)**:
   * جداسازی پردازش‌ها از باینری اصلی با تبدیل Claim و Lockهای داخلی به لایه هماهنگی متکی بر دیتابیس PostgreSQL.
2. **پشتیبانی از S3-Compatible Storage**:
   * اضافه شدن S3 به عنوان `StorageProvider` جدید برای ذخیره‌سازی ابری یا ایجاد نسخه‌های پشتیبان ثانویه.
3. **موتور داخلی Restic**:
   * پیاده‌سازی `ResticBackupEngine` جهت فشرده‌سازی پیشرفته و Deduplication.
4. **توسعه کانکتورها و دیتابیس‌ها**:
   * اضافه کردن کانکتورهای DirectAdmin، Plesk، Windows Agent و پشتیبانی از SQLite، SQL Server، سپیدار و هلو با پیاده‌سازی اینترفیس‌های تفکیک‌شده Capability.
5. **تبدیل به سرویس چندمستأجری ابری (Multi-tenant SaaS Transition)**:
   * فعال‌سازی کامل ثبت‌نام عمومی مشتریان، نقش‌ها و دسترسی‌ها (RBAC)، محدودیت فضا (Storage Quota)، صورت‌حساب (Billing) و پورتال اختصاصی سلف‌سرویس مشتریان.
