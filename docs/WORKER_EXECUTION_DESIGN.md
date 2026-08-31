# طراحی موتور اجرایی و پردازشگرهای پس‌زمینه (Worker & Execution Engine Design)

این سند طراحی جامع معماری لایه پردازش پس‌زمینه، کارگرها (`Backup Workers`)، صف وظایف پایدار (`Job Queue`)، زمان‌بند (`Scheduler`) و چرخه اجرای فرآیندهای پشتیبان‌گیری را بر اساس اسناد [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md)، [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)، [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md) و [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md) مشخص می‌کند.

---

## ۱. بخش اول: نمای کلی معماری اجرا (Execution Architecture Overview)

موتور اجرایی پلتفرم مدیریت پشتیبان‌گیری بر پایه یک معماری لایه‌بندی‌شده و ماژولار است که وظیفه تبدیل درخواست‌های منطقی بکاپ به آرتیفکت‌های پایدار و اعتبارسنجی‌شده را بر عهده دارد.

### جریان داده و کنترل (End-to-End Execution Flow):

```text
[API Layer / Scheduler]
       │
       ▼ (1. Create Job)
[Backup Job Creation] ──────────► (Job Status: pending ── صف پایدار در دیتابیس)
       │
       ▼ (2. Find Pending ➔ Acquire Mutex ➔ Transactional Claim)
[Backup Worker] ────────────────► (Job Status: running ➔ Create BackupRun ➔ Start Heartbeat)
       │
       ├─────────────────────────────────────────┐
       ▼ (3. Authenticate & Connect)             ▼ (4. Stream Pipeline Capability)
[Connector Layer] (SSH / cPanel)          [Direct Stream Backup Engine] (MySQL / Files)
       │                                         │
       └────────────────────┬────────────────────┘
                            │
                            ▼ (5. Stream & Compress)
                     [Storage Layer] (StorageProvider: Local /srv/backup-platform/)
                            │
                            ▼ (6. Integrity & Checksum)
                     [Verification Engine] (SHA-256 & gzip test)
                            │
                            ▼ (7. Finalize Metadata)
                     [Update Status] ──► (Job: completed/failed, Run: success/failed)
```

### ساختار در نسخه ۱ (Modular Monolith Execution):
در نسخه ۱، کلیه این لایه‌ها در قالب یک فرآیند اجرایی واحد به زبان **Go** (`Single Binary Monolith`) به همراه Pool کارگرهای داخلی مبتنی بر Goroutine و مدیریت وظایف پایدار در پایگاه داده PostgreSQL اجرا می‌شوند. کنترل همروندی روی منابع در داخل حافظه همان فرآیند Go از طریق `Per-Resource Mutex` مدیریت می‌شود.

---

## ۲. بخش دوم: طراحی کارگرهای پشتیبان‌گیری (Backup Worker Design)

کارگر (`Backup Worker`) پردازشگر اصلی عملیات پشتیبان‌گیری در پس‌زمینه سیستم است که مسئولیت دریافت، اجرا، نظارت بر حیات و نهایی‌سازی وظایف را بر عهده دارد.

### ترتیب قطعی شروع و اجرای وظیفه توسط کارگر در نسخه ۱:
برای جلوگیری از هرگونه ناهماهنگی و ایجاد رکوردهای معلق، ترتیب شروع اجرای جاب دقیقاً مطابق زنجیره زیر است:

```text
1. Find pending BackupJob
       │
       ▼
2. Acquire Per-Resource Mutex (قفل هم‌زمانی منبع در حافظه فرآیند Go)
       │
       ▼
3. Transactionally verify and claim Job (تأیید وضعیت و تصاحب اتمیک در دیتابیس)
       │
       ▼
4. Change BackupJob status to running
       │
       ▼
5. Create BackupRun with status running & calculated attempt_number
       │
       ▼
6. Start Heartbeat (ارسال پالس حیات و تمدید lease_until روی BackupRun)
       │
       ▼
7. Execute Backup Pipeline (فراخوانی کانکتور و موتور استخراج استریم)
```

> [!IMPORTANT]
> **قواعد انضباطی شروع اجرا در نسخه ۱**:
> * **هیچ `BackupRun` نباید قبل از گرفتن موفق `Per-Resource Mutex` ایجاد شود.**
> * اگر پس از تصاحب Mutex منبع، اعتبارسنجی تراکنشی دیتابیس (`Transactional Claim`) به دلیل لغو هم‌زمان توسط کاربر یا تغییر وضعیت شکست بخورد، `Per-Resource Mutex` بلافاصله آزاد می‌شود و هیچ رکوردی در جدول `backup_runs` ایجاد نخواهد شد.
> * این مدل قفل‌گذاری مبتنی بر Mutex درون‌فرآیندی منحصراً برای نسخه تک‌فرآیندی ۱ (`Single-Process Monolith`) طراحی شده است و قفل‌های توزیع‌شده دیتابیس (`Distributed Resource Lock`) صرفاً معماری آینده برای چند نود مجزا خواهد بود.

### تفکیک کارگر از منطق وب و احراز هویت (Decoupling Principle):
* کارگر **هیچ‌گونه وابستگی** به لایه HTTP، کنترلرهای API، نشست‌های کاربر، کوکی‌ها یا JWT ندارد.
* ارتباط کارگر منحصراً از طریق **Service Layer** و واسط‌های دامنه‌ای Go انجام می‌پذیرد.
* کارگر تمام کانتکست‌های مورد نیاز خود (از جمله `organization_id`، `resource_id` و پیکربندی منبع) را از موجودیت‌های ذخیره‌شده جاب دریافت می‌کند.

---

## ۳. بخش سوم: طراحی صف وظایف داخلی (Job Queue Design)

در نسخه ۱، رکوردهای جدول `backup_jobs` با وضعیت `pending` در پایگاه داده PostgreSQL نقش صف پایدار (`Durable Queue`) را ایفا می‌کنند و نیازی به ابزارهای جانبی پیچیده نظیر Kafka یا RabbitMQ نیست.

### ماشین وضعیت جاب (BackupJob State Machine):

```text
                       ┌───────────────┐
                       │    pending    │ ◄── (ایجاد اولیه یا Retry پس از شکست موقت)
                       └───────┬───────┘
                               │
                ┌──────────────┼──────────────┐
                │ (لغو توسط کاربر)             │ (تصاحب توسط کارگر / قفل Mutex)
                ▼                             ▼
        ┌───────────┐                 ┌───────────────┐
        │ cancelled │                 │    running    │
        └───────────┘                 └───────┬───────┘
                                              │
                               ┌──────────────┴──────────────┐
                               │                             │
                               │ (موفقیت Run)                │ (خطای نهایی یا بدون Retry)
                               ▼                             ▼
                        ┌───────────┐                 ┌───────────┐
                        │ completed │                 │  failed   │
                        └───────────┘                 └───────────┘
                               ▲
                               │
               (در صورت خطای Retryable: بازگشت به pending)
```

> [!NOTE]
> **یکپارچگی وضعیت‌ها و محدودیت لغو در نسخه ۱**:
> * وضعیت‌های مجاز `BackupJob` منحصراً ۵ حالت `pending`, `running`, `completed`, `failed`, `cancelled` هستند. وضعیت موفقیت تلاش اجرایی در موجودیت `BackupRun` برابر با `success` است.
> * در نسخه ۱، عملیات لغو جاب (`Cancellation`) **صرفاً قبل از شروع اجرای واقعی** و در وضعیت `pending` امکان‌پذیر است (`pending ➔ cancelled`). لغو یک Run در حال اجرای فعال (`running`) در نسخه ۱ طراحی نشده است.

### قوانین انتقال وضعیت جاب:

| وضعیت مبدأ | وضعیت مقصد | عامل مجاز تغییر | شرح و شرایط انتقال |
| :--- | :--- | :--- | :--- |
| `None` | `pending` | **API / Scheduler** | ایجاد اولیه درخواست منطقی بکاپ در صف پایدار دیتابیس. |
| `pending` | `running` | **Backup Worker** | تصاحب جاب توسط کارگر پس از اخذ `Per-Resource Mutex` و ایجاد رکورد `BackupRun`. |
| `running` | `completed` | **Backup Worker** | اتمام موفقیت‌آمیز، تولید و اعتبارسنجی آرتیفکت، و آزادسازی Mutex. |
| `running` | `pending` | **Backup Worker / Reaper** | **انتقال مجاز برای تلاش مجدد (Retry)**: بروز خطای گذرا و موقت (Retryable)؛ جاب به صف پایدار بازمی‌گردد تا کارگر بعداً تلاش اجرایی جدیدی با `attempt_number` بعدی بسازد. |
| `running` | `failed` | **Backup Worker / Reaper** | بروز خطای غیرقابل بازیابی (Non-retryable) یا اتمام سقف مجاز Retry. |
| `pending` | `cancelled` | **User (Admin)** | لغو جاب در صف توسط کاربر مجاز قبل از شروع اجرای واقعی توسط کارگر. |

### مفهوم تصاحب امن وظیفه (Transactional Job Claiming):
کارگرهای فعال در فرآیند Go برای جلوگیری از Race Condition و تداخل در انتخاب وظایف، عملیات خواندن جاب‌های `pending` و تغییر وضعیت به `running` را به صورت اتمیک و در قالب یک تراکنش پایگاه داده انجام می‌دهند تا تضمین شود هر جاب دقیقاً توسط یک کارگر پردازش می‌گردد.

---

## ۴. بخش چهارم: طراحی زمان‌بند پشتیبان‌گیری (Scheduler Design)

زمان‌بند (`Scheduler`) یک کامپوننت پس‌زمینه سبک در فرآیند Go است که به صورت چرخه‌ای وضعیت برنامه‌های پشتیبان‌گیری (`BackupPlan`) را بررسی می‌کند.

### وظایف زمان‌بند:
1. **بررسی Planهای فعال**: شناسایی برنامه‌هایی که `status = 'active'` و `schedule_enabled = true` دارند.
2. **محاسبه زمان سررسید (Next Run Evaluation)**: انطباق زمان سرور با `schedule_cron` بر اساس `schedule_timezone` تعریف‌شده در Plan.
3. **تولید جاب منطقی (`Create BackupJob`)**: ایجاد یک رکورد جدید `BackupJob` در وضعیت `pending` با ارجاع به `backup_plan_id`.
4. **رفتار زمان‌بند در زمان مشغول بودن منبع (Resource Busy Handling)**: اگر موعد اجرای یک Plan فرا برسد در حالی که منبع به دلیل اجرای جاب دیگری مشغول است، زمان‌بند جاب را **حذف یا Skip نمی‌کند**؛ بلکه یک جاب پایدار در وضعیت `pending` ایجاد می‌نماید تا به محض آزاد شدن منبع توسط کارگر اجرا شود.
5. **جلوگیری از ایجاد جاب‌های تکراری مکرر (Deduplication)**: در صورتی که جاب سررسید قبلی متعلق به همان Plan هنوز در وضعیت `pending` باقی مانده باشد، از تولید جاب‌های تکراری پیاپی خودداری می‌شود.
6. **مشتق‌سازی تاریخچه**: تاریخچه آخرین اجرا (`last_run_at`) در برنامه‌ها به صورت پویا از آخرین Job/Run مرتبط مشتق می‌شود و فیلد افزونه‌ای در دیتابیس ایجاد نمی‌گردد.

> [!IMPORTANT]
> **تفکیک قطعی مسئولیت زمان‌بند**:
> زمان‌بند **هرگز** مستقیماً بکاپ اجرا نمی‌کند، اتصال SSH برقرار نمی‌سازد یا داده‌ها را جابجا نمی‌کند. وظیفه زمان‌بند **صرفاً ایجاد رکورد جاب پایدار (`pending`)** است.

---

## ۵. بخش پنجم: چرخه حیات تلاش اجرایی (BackupRun Lifecycle)

یک `BackupJob` دارای یک یا چند تلاش اجرایی (`BackupRun`) است. هر تلاش از ۸ مرحله پیاپی و شفاف عبور می‌کند:

```text
1. Acquire Mutex ──► 2. Claim Job ──► 3. Create Run ──► 4. Prepare Env & Heartbeat
                                                                  │
8. Update Result ◄── 7. Release Mutex ◄── 6. Verify ◄── 5. Execute Pipeline
```

### شرح تفصیلی مراحل اجرای Run:

1. **اخذ قفل هم‌زمانی منبع (`Acquire Per-Resource Mutex`)**:
   * تصاحب Mutex مربوط به `resource_id` در حافظه فرآیند Go.
   * *در صورت مشغول بودن منبع*: بازگرداندن خطای تداخل (`409 Conflict` برای درخواست‌های دستی) یا باقی ماندن در صف `pending`.
2. **تصاحب تراکنشی جاب (`Transactionally Claim Job`)**:
   * بررسی اتمیک وضعیت `pending` جاب و تغییر وضعیت آن به `running`.
   * *در صورت عدم موفقیت در Claim*: آزادسازی فوری Mutex بدون ایجاد Run.
3. **ایجاد تلاش اجرایی (`Create Run`)**:
   * ثبت رکورد جدید در جدول `backup_runs` با `status = 'running'`، زمان شروع (`started_at = NOW()`) و شماره تلاش محاسبه‌شده (`attempt_number`).
4. **آماده‌سازی محیط و فعال‌سازی Heartbeat**:
   * ایجاد دایرکتوری موقت با دسترسی محدود `0700` در `/srv/backup-platform/tmp/run-{id}`.
   * راه‌اندازی روتین ارسال Heartbeat برای تمدید منظم `lease_until` رکورد `BackupRun`.
   * واکشی و رمزگشایی سکرت‌ها با حداقل طول عمر در محدوده تابعی (`Function-scoped`).
5. **اجرای پایپ‌لاین پشتیبان‌گیری و تولید آرتیفکت (`Execute Pipeline & Artifact Generation`)**:
   * برقراری اتصال توسط درایور کانکتور (SSH یا cPanel API).
   * اجرای استریم قابلیت مورد نظر (MySQL Dump یا آرشیو فایل‌ها) توسط `Direct Stream Backup Engine`.
   * فشرده‌سازی هم‌زمان استریم با `gzip` و انتقال به `StorageProvider` (فایل‌سیستم محلی با مجوزهای دایرکتوری `0700` و فایل `0600`).
   * ثبت متادیتای آرتیفکت در جدول `backup_artifacts` (شامل `size_bytes` و `checksum_sha256`).
6. **اعتبارسنجی سلامت (`Verify Artifact`)**:
   * تطبیق هش SHA-256 و تست یکپارچگی فایل فشرده (`gzip -t` یا `tar -tzf`).
   * به‌روزرسانی `verification_status` به `verified`.
7. **آزادسازی قفل و پاک‌سازی (`Release Mutex & Cleanup`)**:
   * پاک‌سازی فایل‌های موقت، بازنویسی غیرتضمینی سکرت‌ها در حافظه (`best-effort zeroization`)، و آزادسازی `Per-Resource Mutex`.
8. **ثبت نهایی نتیجه (`Update Result`)**:
   * تغییر وضعیت `BackupRun` به `success` (یا `failed`) و تنظیم `ended_at`. (مقدار `duration` در زمان نمایش از تفاضل `ended_at - started_at` محاسبه می‌شود).
   * تغییر وضعیت `BackupJob` به `completed` (یا در صورت شکست موقت بازگشت به `pending` برای Retry).
   * اعمال سیاست نگهداری (`Retention Policy`) در صورت موفقیت کامل.

---

## ۶. بخش ششم: کنترل همروندی و بازیابی پس از خرابی (Concurrency Control & Crash Recovery)

### ۱. کنترل همروندی در نسخه ۱ (In-Process Per-Resource Mutex):
* در معماری Modular Monolith نسخه ۱، پیشگیری از تداخل اجرای هم‌زمان روی یک سرور از طریق **`Per-Resource Mutex`** در حافظه برنامه Go پیاده‌سازی می‌شود.
* هر تلاش جدید برای اجرای بکاپ روی منبعی که Mutex آن در وضعیت Lock است، رد شده یا در صف انتظار باقی می‌ماند.

### ۲. کاربرد فیلدهای `heartbeat_at` و `lease_until` در `BackupRun`:
* این فیلدها در جدول `backup_runs` قرار دارند و برای **اثبات مالکیت اجرای فعال و تشخیص خرابی/کرش (Stale Run Detection)** استفاده می‌شوند، نه به عنوان قفل توزیع‌شده منابع.
* **الگوی سازگار زمان‌بندی**:
  * کارگر در حین اجرا هر **۳۰ ثانیه** فیلد `heartbeat_at` را به‌روزرسانی کرده و مقدار `lease_until` را به **۲ دقیقه بعد** (`NOW() + 2 minutes`) تمدید می‌کند.
  * در صورتی که کارگر کرش کند یا سرور ریستارت شود، ارسال Heartbeat متوقف می‌گردد.

### ۳. فرآیند پاک‌سازی و بازیابی جاب‌های معلق (Zombie Run Reaper):
* یک فرآیند ناظر دوره‌ای در پس‌زمینه (`Reaper`) اجراهایی را که `status = 'running'` هستند اما مهلت Lease آن‌ها منقضی شده است (`lease_until < NOW()`) شناسایی می‌کند.
* فرآیند Reaper وضعیت Run قبلی را به `failed` با پیام پاک‌سازی‌شده `Worker lease expired / Process terminated unexpectedly` تغییر داده و جاب را در صورت مجاز بودن سیاست Retry به `pending` بازمی‌گرداند.
* تاریخچه تلاش‌های قبلی هرگز حذف یا بازنویسی نمی‌شود تا تاریخچه ممیزی کاملاً پایدار بماند.

> [!NOTE]
> استفاده از قفل‌های اجاره‌ای توزیع‌شده در دیتابیس (`DB-backed Distributed Resource Lease`) صرفاً مسیر توسعه آینده برای پشتیبانی از چند نود کارگر مجزا (`Multiple Worker Instances`) خواهد بود.

---

## ۷. بخش هفتم: استراتژی تلاش مجدد (Retry Strategy)

سیاست تلاش مجدد بر پایه ارزیابی ماهیت خطاها و سوابق اجراهای گذشته پیاده‌سازی می‌شود.

### قوانین و جریان انتقال وضعیت تلاش مجدد (Retry Flow):
1. در صورت بروز خطای قابل تلاش مجدد (`Retryable Failure`)، وضعیت `BackupRun` فعال به `failed` تغییر می‌یابد.
2. اگر تعداد تلاش‌های انجام‌شده از سقف مجاز کمتر باشد، وضعیت `BackupJob` از `running` به **`pending`** بازگردانده می‌شود.
3. کارگر در چرخه بعدی، جاب `pending` را تحویل گرفته و یک تلاش اجرایی جدید (`BackupRun`) با `attempt_number` بعدی ایجاد می‌کند.
4. **عدم حذف یا استفاده مجدد از Run قبلی**: رکوردهای تلاش‌های گذشته هرگز حذف یا بازنویسی (`Reuse`) نمی‌شوند.
5. **تعداد تلاش‌ها (`attempt_number`)**: از شمارش تعداد رکوردهای `BackupRun` ثبت‌شده برای همان `job_id` محاسبه می‌گردد.
6. **سقف تلاش مجدد (`max_retry`)**: یک پارامتر تنظیمی در سطح سیاست‌های برنامه (`Application Policy`) است (پیش‌فرض: ۳ بار).
7. **زمان‌بندی تلاش بعدی**: زمان تلاش بعدی بر اساس زمان پایان آخرین Run و فرمول وقفه نمایی فزاینده (`Exponential Backoff with Jitter`) در سطح کد محاسبه می‌شود (بدون نیاز به فیلد اضافی در دیتابیس):
   $$\text{Delay} = \min(2^{\text{attempt\_number}} \times 30\text{s} + \text{random\_jitter}, 10\text{m})$$

### تفکیک خطاهای قابل تلاش مجدد و خطاهای دائمی:

| دسته‌بندی خطا | نمونه خطاها | قابلیت Retry؟ | اقدام کارگر |
| :--- | :--- | :---: | :--- |
| **خطاهای موقت شبکه و I/O** | `Connection timeout`, `SSH handshake drop`, `Storage I/O temporary busy` | ✅ بله | علامت‌گذاری Run جاری به `failed`، بازگرداندن Job به `pending` و برنامه‌ریزی تلاش بعدی. |
| **خطاهای احراز هویت و دسترسی** | `Authentication failed`, `Invalid SSH Key`, `Access denied for MySQL user`, `Permission denied` | ❌ خیر | علامت‌گذاری Run به `failed` و تنظیم وضعیت نهایی Job به `failed` بدون تلاش مجدد. |
| **خطاهای منطقی پیکربندی** | `Database not found`, `Invalid file path`, `Syntax error in plan config` | ❌ خیر | توقف فوری و تنظیم وضعیت نهایی Job به `failed`. |

---

## ۸. بخش هشتم: جریان اجرای اتصال‌دهنده‌ها (Connector Execution Flow)

اتصال‌دهنده‌ها (`Connectors`) لایه تطبیق‌دهنده ارتباط شبکه با سرورهای هدف هستند.

```text
[Backup Worker]
       │ (1. دریافت مشخصات اتصال و شناسه Credential)
       ▼
[Credential Service] ──► (2. رمزگشایی AES-256-GCM در حافظه RAM)
       │
       ▼ (3. تحویل سکرت به نمونه درایور با حداقل طول عمر)
[Connector Driver]
       │
       ├──────────────────────────────────────────┐
       ▼ (منبع لینوکس)                             ▼ (هاست اشتراکی)
[Ubuntu SSH Driver]                        [cPanel API Driver]
  • نشست SSH امن با کلید خصوصی               • ارتباط امن HTTPS با API Token
  • بررسی Host Key Fingerprint              • استخراج استریم داده از UAPI
```

### ضوابط مدیریت سکرت‌ها در حافظه کارگر (Secret Memory Handling):
* سیستم از اصول **`minimum secret lifetime`** (حداقل طول عمر سکرت) و **`function-scoped secret handling`** (محدودسازی دسترسی سکرت به دامنه تابع فراخوان) پیروی می‌کند.
* پس از اتمام استفاده، پاک‌سازی به صورت **`best-effort zeroization`** انجام می‌شود؛ با این توضیح فنی که به دلیل مکانیزم مدیریت حافظه و Garbage Collection در زمان اجرای زبان Go، عدم وجود کامل ردپا در اعماق حافظه به صورت صددرصدی گارانتی نمی‌شود اما بالاترین سطح مراقبت اعمال می‌گردد.

---

## ۹. بخش نهم: یکپارچه‌سازی موتور پشتیبان‌گیری و ذخیره‌سازی (Backup Engine & Storage Integration)

معماری پلتفرم، لایه هماهنگ‌کننده کارگر را از انتزاع موتور بکاپ (`BackupEngine`) و ارائه‌دهنده ذخیره‌سازی (`StorageProvider`) کاملاً مجزا نگه می‌دارد.

### ۱. انتزاع موتور پشتیبان‌گیری (`BackupEngine` Abstraction):
* **در نسخه ۱**: استفاده از **`Direct Stream Backup Engine`** که داده‌ها را مستقیماً از کانکتور به خط لوله فشرده‌سازی استریم می‌کند.
  * **قابلیت MySQL Dump (`mysql_database`)**: اجرای `mysqldump` با فلگ‌های بهینه‌ساز بدون قفل (`--single-transaction --quick --routines --triggers`).
  * **قابلیت آرشیو فایل‌ها (`website_files`)**: بسته‌بندی فایل‌های وب‌سایت با `tar` همراه با فیلترهای استثنا (`exclude patterns`).
* **در نسخه‌های آینده**: پشتیبانی از موتورهای پیشرفته نظیر **`restic`** جهت پشتیبان‌گیری افزایشی (Incremental/Deduplicated).

### ۲. انتزاع ارائه‌دهنده ذخیره‌سازی (`StorageProvider` Abstraction):
* **در نسخه ۱**: **`Local Storage Provider`** با مسیر پیش‌فرض `/srv/backup-platform/artifacts/`.
* **در نسخه‌های آینده**: ارائه‌دهنده‌های ذخیره‌سازی ابری مانند S3 و S3-Compatible Storage.

### ۳. امنیت و رمزگذاری آرتیفکت‌ها در حالت سکون (Artifact Encryption at Rest):
* در نسخه ۱، آرتیفکت‌های Direct Stream ذخیره‌شده بر روی دیسک محلی ممکن است در لایه داده به صورت رمزگذاری‌شده نباشند و امنیت آن‌ها از طریق مجوزهای سخت‌گیرانه لینوکس تأمین می‌شود:
  * دایرکتوری‌های ذخیره‌سازی: **`0700`** (فقط کاربر سرویس).
  * فایل‌های آرتیفکت بکاپ: **`0600`** (فقط خواندن و نوشتن برای مالک).
* پیاده‌سازی رمزگذاری در حالت سکون (Encryption at Rest) برای فایل‌های بکاپ، یک الزام تولیدی (`Production Requirement`) قبل از ارائه نسخه عمومی SaaS است و موتورهایی نظیر `restic` یکی از گزینه‌های اصلی برای تحقق این هدف خواهند بود.

---

## ۱۰. بخش دهم: ثبت رویدادها و مانیتورینگ کارگر (Logging & Monitoring)

ثبت وقایع لایه کارگر باید ساختاریافته (Structured JSON) و کاملاً پاک‌سازی‌شده از داده‌های حساس باشد.

### اقلام مجاز در لاگ‌های ساختاریافته و پاسخ‌های تحلیلی API:
* `job_id` و `run_id`: شناسه‌های رهگیری فرآیند.
* `organization_id` و `resource_id`: شناسه‌های سازمانی و منبع.
* `worker_id`: شناسه نام‌گذاری‌شده کارگر در زمان اجرا (Runtime Log Metadata - بدون نیاز به فیلد دیتابیس).
* `backup_type`: نوع عملیات (`mysql_database` یا `website_files`).
* `start_time` و `end_time`: زمان‌های ثبت‌شده در دیتابیس.
* `duration_seconds`: **مقدار مشتق‌شده** از تفاضل `ended_at - started_at` (بدون فیلد مجزا در جدول `backup_runs`).
* `size_bytes`: متعلق به موجودیت `BackupArtifact` (در صورت نیاز به سایز کل Run، از مجموع سایز آرتیفکت‌های مرتبط مشتق می‌شود).
* `status`: وضعیت نهایی اجرا.
* `sanitized_error_message`: پیام‌های خطای استاندارد و امن.

> [!CAUTION]
> ثبت پسوردها، کلیدهای خصوصی SSH، توکن‌های دسترسی یا محتوای جداول دیتابیس مشتریان در لاگ‌ها اکیداً ممنوع است.

---

## ۱۱. بخش یازدهم: مدیریت انواع شکست و سناریوهای خطا (Failure Handling)

| سناریوی خطا | نحوه تشخیص (Detection) | اقدام کارگر (Action) | وضعیت نهایی جاب / ران |
| :--- | :--- | :--- | :--- |
| **۱. قطعی شبکه (Network Failure)** | دریافت خطای `TCP timeout` یا `broken pipe` در حین استریم. | بستن سوکت، حذف فایل موقت ناقص، ثبت خطای شبکه و بازگرداندن Job به `pending` جهت تلاش مجدد. | Run: `failed`<br>Job: `pending` (در انتظار Retry) یا `failed` |
| **۲. شکست احراز هویت SSH** | دریافت خطای `ssh: unable to authenticate`. | توقف فوری (بدون Retry)، آزادسازی Mutex منبع، ثبت خطای اعتبار سنجی. | Run: `failed`<br>Job: `failed` |
| **۳. خطای Dump دیتابیس** | خروج `mysqldump` با کد غیر صفر یا خطای ساختار دیتابیس. | حذف آرتیفکت ناقص، ثبت خطای پایگاه داده و آزادسازی Mutex منبع. | Run: `failed`<br>Job: `failed` |
| **۴. پر شدن دیسک سرور (Disk Full)** | دریافت خطای `ENOSPC` از سیستم‌عمل. | توقف فوری، حذف سریع فایل‌های موقت، صدور آلرت بحرانی و توقف اجرای جاب‌های جدید. | Run: `failed`<br>Job: `failed` |
| **۵. کرش ناگهانی کارگر (Worker Crash)** | انقضای `lease_until` روی رکورد `BackupRun` بدون دریافت Heartbeat. | شناسایی توسط ناظر دوره‌ای (`Zombie Reaper`)، علامت‌گذاری Run قبلی به عنوان `failed` و بازگرداندن جاب به `pending` در صورت امکان. | Run: `failed`<br>Job: `pending` یا `failed` |
| **۶. راه‌اندازی مجدد سرور (Server Restart)** | باقی ماندن جاب‌ها در وضعیت `running` پس از بوت پلتفرم. | بازنشانی در زمان راه‌اندازی اولیه سرور (Boot Hook) و آماده‌سازی جاب‌های نیمه‌تمام با بازگرداندن به `pending`. | Run: `failed`<br>Job: `pending` |

---

## ۱۲. بخش دوازدهم: مسیر توسعه و مقیاس‌پذیری آینده (Future Scaling Path)

معماری موتور اجرایی به گونه‌ای است که بدون برهم زدن ساختار نسخه ۱ (`Modular Monolith`)، مسیر توسعه به سمت سیستم‌های توزیع‌شده را هموار می‌کند:

```text
[نسخه ۱: Modular Monolith]
┌─────────────────────────────────────────────────────────────────┐
│  Go App Binary (In-Process Per-Resource Mutex + DB Claim Queue) │
└─────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼ (مسیر توسعه آینده)
[نسخه ۲: Multiple Distributed Workers]
┌─────────────┐        ┌──────────────────┐        ┌──────────────────┐
│  API Nodes  │ ─────► │ Distributed Queue│ ─────► │ Worker Instances │
│  (Web/SaaS) │        │ (DB Lease/Redis) │        │ (Backup Engine)  │
└─────────────┘        └──────────────────┘        └──────────────────┘
                                 │
                                 ▼ (مسیر توسعه ابری / سازمانی)
[نسخه ۳: Remote Backup Agents & Incremental Restic Engine]
┌──────────────────┐                               ┌──────────────────┐
│ Central Platform │ ◄─────── (gRPC / TLS) ──────► │ Remote Agent     │
│ (Orchestrator)   │                               │ (On Customer VM) │
└──────────────────┘                               └──────────────────┘
```

---

## ۱۳. بخش سیزدهم: خلاصه تصمیمات کلیدی طراحی موتور اجرایی (Design Decisions Summary)

| حوزه معماری | تصمیم اتخاذشده | دلیل و منطق فنی |
| :--- | :--- | :--- |
| **Worker Architecture** | کارگرهای هم‌روند داخلی (Goroutine Worker Pool) درون Modular Monolith با تفکیک کامل از API | سادگی در استقرار نسخه ۱، مصرف بهینه منابع سرور و آمادگی برای مقیاس‌پذیری در آینده. |
| **Queue Strategy** | صف پایدار بر پایه وضعیت `pending` در دیتابیس PostgreSQL با تصاحب تراکنشی اتمیک | حذف وابستگی‌های سنگین خارجی (Kafka/RabbitMQ)، پایداری در برابر ریستارت و یکپارچگی تراکنشی. |
| **Execution Start Flow** | ترتیب قطعی: یافتن جاب ➔ اخذ Mutex ➔ تصاحب تراکنشی ➔ وضعیت running جاب ➔ ایجاد Run ➔ شروع Heartbeat ➔ اجرای استریم | پیشگیری قطعی از تداخل هم‌زمانی و عدم ایجاد Run پیش از اخذ موفق قفل در نسخه تک‌فرآیندی V1. |
| **Retry State Transition** | شکست Run به `failed` و بازگشت وضعیت Job از `running` به `pending` برای ایجاد Run جدید با `attempt_number` بعدی | حفظ کامل تاریخچه کلیه تلاش‌های گذشته بدون حذف یا استفاده مجدد، و تکیه بر صف پایدار برای اجراهای مجدد. |
| **Cancellation Rule** | محدودیت لغو جاب صرفاً قبل از شروع اجرا (`pending ➔ cancelled`) در نسخه ۱ | سادگی معماری V1 و عدم پیچیده‌سازی مدیریت نشست‌های قطع‌نشده SSH یا فرایندهای استریم در حال اجرا. |
| **Data Model Fidelity** | مشتق‌سازی `duration_seconds` از تفاضل زمان‌ها و انتساب `size_bytes` منحصراً به `BackupArtifact` | تطابق کامل با `DATA_MODEL.md` و عدم افزودن فیلدهای افزونه‌ای و تکراری به جداول دیتابیس. |
| **Scheduler Responsibility** | انحصار نقش زمان‌بند به ایجاد جاب پایدار (`pending`)؛ بدون لغو جاب در زمان مشغول بودن منبع | تفکیک دقیق وظایف، عدم از دست رفتن زمان‌بندی‌ها در زمان بار سنگین، و عدم اجرای مستقیم بکاپ توسط زمان‌بند. |
| **Locking Model** | کنترل همروندی روی منبع با `Per-Resource Mutex` در حافظه فرآیند Go در V1 | سادگی و کارایی بالا در تک‌باینری، مهار هم‌زمانی روی سرور مشتری، و ثبت `heartbeat/lease` روی Run صرفاً جهت رهگیری مالکیت و کشف کرش. |
| **Engine & Storage** | تفکیک انتزاع `BackupEngine` (موتور Direct Stream در V1 و Restic در آینده) از `StorageProvider` | انعطاف‌پذیری در پشتیبانی از انواع بکاپ و مقاصد ذخیره‌سازی، و تعیین رمزگذاری در سکون به عنوان پیش‌نیاز نسخه Public SaaS. |
| **Memory Safety** | پاک‌سازی سکرت‌ها با رویکرد Best-effort Zeroization، حداقل طول عمر و دسترسی Function-scoped | رعایت اصول امنیتی متناسب با ویژگی‌های زمان اجرای زبان Go بدون ادعاهای غیرواقعی. |
