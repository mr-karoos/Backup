# ثبت رسمی تصمیمات معماری (Architecture Decision Records - ADR)

این سند مرجع رسمی و ثبت‌شده کلیه تصمیمات معماری و فنی کلیدی پلتفرم مدیریت پشتیبان‌گیری (`Backup Platform`) بر اساس اسناد مرجع [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md)، [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md)، [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)، [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md)، [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md)، [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md) و [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md) است.

هر تصمیم ثبت‌شده در وضعیت `Accepted` مبنای قطعی توسعه است و هرگونه تغییر آتی مستلزم تدوین و تصویب ADR جدید خواهد بود.

---

## ADR-001 — انتخاب زبان برنامه‌نویسی سمت سرور (Programming Language)

* **Status**: `Accepted`
* **Context**: سیستم نیازمند یک زبان مدرن، پایدار و کارآمد برای پردازش هم‌روند وظایف پس‌زمینه (Worker Pool)، مدیریت استریم‌های سنگین شبکه/فایل‌سیستم و استقرار آسان روی سرور لینوکس ابونتو است.
* **Decision**: باینری بک‌اند پلتفرم با زبان برنامه‌نویسی **Go** پیاده‌سازی می‌شود.
* **Rationale**:
  * کارایی و سرعت بالای اجرا و مدیریت همروندی بهینه با Goroutines برای پردازشگرهای پشتیبان‌گیری.
  * استقرار به صورت تک‌باینری مستقل (**`Single Binary Deployment`**) جهت سادگی استقرار عملیاتی بدون پیچیدگی‌های محیطی.
  * مصرف بسیار بهینه حافظه رم و پردازنده متناسب با منابع سرور میزبان (8 Cores, 16 GB RAM).
  * سازگاری و کتابخانه‌های قدرتمند بومی برای ارتباطات شبکه (SSH Client, HTTP/REST) و استریمینگ I/O.
* **Consequences / Trade-offs**: عدم وجود فریم‌ورک‌های سنگین با ویژگی‌های پنهان؛ نیازمند معماری صریح، انضباط در مدیریت حافظه و هندلینگ دستی خطاها. (الزام پیوند استاتیک به عنوان شرط قطعی در نظر گرفته نمی‌شود چون به Build Configuration و وضعیت CGO بستگی دارد).
* **Related Documents**: [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md), [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md)

---

## ADR-002 — معماری کلی برنامه در نسخه ۱ (Application Architecture)

* **Status**: `Accepted`
* **Context**: برای نسخه اولیه، ساختار نرم‌افزار باید ساده، قابل اعتماد، سریع در استقرار و بدون بار عملیاتی سنگین باشد، در حالی که در کد تفکیک ماژولار حفظ شود تا گذار به سیستم‌های توزیع‌شده در آینده ممکن باشد.
* **Decision**: معماری سیستم در نسخه ۱ به صورت **`Modular Monolith`** پیاده‌سازی می‌شود. کلیه ماژول‌ها درون یک باینری و پروسس واحد اجرا می‌شوند. از مایکروسرویس‌ها، Kafka یا Kubernetes در نسخه ۱ استفاده نخواهد شد.
* **Rationale**:
  * پرهیز از پیچیدگی‌های زودهنگام شبکه، هماهنگی توزیع‌شده و دیباگ چندسرویسی در فاز اولیه.
  * تفکیک ماژولار منطقی در سطح کدهای Go (پکیج‌های مجزا) برای حفظ مرزهای دامنه‌ای.
  * استقرار، نگهداری و خطایابی بسیار آسان بر روی یک سرور واحد.
* **Consequences / Trade-offs**: مقیاس‌پذیری افقی در نسخه ۱ محدود به منابع همان سرور است؛ جداسازی نودها به نسخه‌های بعدی موکول می‌شود.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-003 — پایگاه داده متادیتا و صف وظایف (Metadata Database)

* **Status**: `Accepted`
* **Context**: پلتفرم نیازمند پایگاه داده رابطه‌ای سازگار با ACID برای ذخیره اطلاعات کاربران، سازمان‌ها، منابع، برنامه‌ها، تاریخچه بکاپ‌ها و مدیریت صف پایدار وظایف است.
* **Decision**: پایگاه داده **`PostgreSQL`** به عنوان منبع واحد حقیقت متادیتا و بستر صف پایدار وظایف (`Backup Jobs`) انتخاب شد.
* **Rationale**:
  * تضمین قوی یکپارچگی تراکنشی (ACID) و پشتیبانی عالی از کلیدهای خارجی و قفل‌های سطری.
  * پشتیبانی عالی از نوع داده `JSONB` برای نگهداری پیکربندی‌های انعطاف‌پذیر کانکتورها و اسنپ‌شات‌ها.
  * پایداری اثبات‌شده و ابزارهای مانیتورینگ و بکاپ‌گیری استاندارد.
* **Consequences / Trade-offs**: وابستگی اجرایی باینری Go به سرویس PostgreSQL؛ نیاز به مدیریت منظم ایندکس‌ها و لاگ‌های WAL.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md)

---

## ADR-004 — مدل چندمستأجری و ایزولاسیون سازمان‌ها (Multi-Tenancy Model)

* **Status**: `Accepted`
* **Context**: پلتفرم باید از روز اول ساختار تفکیک سازمان‌ها را رعایت کند تا در آینده بدون بازطراحی اساسی به یک SaaS عمومی تبدیل شود.
* **Decision**: مدل چندسازمانی بر پایه ستون **`organization_id`** در کلیه موجودیت‌های متعلق به مستأجر پیاده‌سازی می‌شود. موجودیت `users` در سطح سیستم تعریف شده و عضویت کاربران در سازمان‌ها از طریق جدول رابط `organization_members` کنترل می‌گردد. دسترسی متقاطع (Cross-Org) اکیداً مسدود است و اولین سازمان داخلی در مرحله Initial Setup راه‌اندازی (Bootstrap) می‌شود.
* **Rationale**:
  * معماری ساده و یکپارچه در سطح پایگاه داده مشترک با تفکیک منطقی سخت‌گیرانه در Service Layer.
  * امکان عضویت یک کاربر در چند سازمان با نقش‌های متفاوت.
  * سادگی بسیار بیشتر نسبت به مدل Database-per-tenant برای نسخه ۱.
* **Consequences / Trade-offs**: ضرورت اعمال فیلتر `organization_id` در کلیه کوئری‌های واکشی و تغییر داده‌ها و تست مستمر نشت اطلاعات میان مستأجران.
* **Related Documents**: [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md), [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)

---

## ADR-005 — راهبرد و استانداردهای طراحی رابط برنامه‌نویسی (API Strategy)

* **Status**: `Accepted`
* **Context**: کلاینت‌های وب و ابزارهای مدیریتی نیازمند یک رابط استاندارد، امن و نسخه‌بندی‌شده برای تعامل با بک‌اند پلتفرم هستند.
* **Decision**: وب‌سرویس به صورت **REST API** با مسیر پایه **`/api/v1`** پیاده‌سازی می‌شود. ساختار پاسخ‌ها و خطاها استاندارد و یکنواخت بوده و شامل شناسه یکتای درخواست (`request_id`) است. هیچ سکرت، پسورد یا مسیر فیزیکی فایل‌سیستم سرور در خروجی APIها افشا نمی‌شود.
* **Rationale**:
  * سادگی یکپارچه‌سازی با انواع کلاینت‌های فرانت‌اند و اسکریپت‌های اتوماسیون.
  * شفافیت در قرارداد داده‌ها و کدهای وضعیت استاندارد HTTP.
  * ردیابی دقیق خطاها و لاگ‌ها از طریق `request_id`.
* **Consequences / Trade-offs**: نیاز به لایه DTO و مپینگ دقیق داده‌ها میان لایه مدل دیتابیس و لایه نمایش API جهت جلوگیری از افشای فیلدهای حساس.
* **Related Documents**: [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md)

---

## ADR-006 — راهبرد احراز هویت و مدیریت نشست‌ها (Authentication Strategy)

* **Status**: `Accepted`
* **Context**: سیستم نیازمند مدل احراز هویت امن بر بستر وب با توکن‌های کوتاه‌مدت، حفاظت در برابر حملات XSS/CSRF، و امکان ابطال بلادرنگ نشست‌ها (Immediate Server-side Revocation) توسط کاربر یا مدیر سیستم است.
* **Decision (جریان تثبیت‌شده توکن‌ها و نشست‌ها)**:
  * **Access Token**: توکن مبتنی بر JWT کوتاه‌مدت (۱۵ دقیقه) که از طریق هدر استاندارد `Authorization: Bearer <token>` در درخواست‌های API ارسال می‌شود و شامل کلیم‌های ضروری `user_id`، `session_id` و `is_system_admin` است.
  * **Refresh / Session Token**: توکن تصادفی کدر و با آنتروپی بالا (Opaque Random Token) با طول عمر ۷ روز که منحصراً در کوکی امن `HttpOnly` با فلگ‌های `SameSite=Strict` و `Secure` (در زمان فعال بودن HTTPS) نگهداری می‌شود.
  * **مدل ذخیره‌سازی پایدار در دیتابیس (`user_sessions`)**: توکن خام رفرش هرگز در دیتابیس ذخیره نمی‌شود و صرفاً هش یک‌طرفه امن آن در فیلد `refresh_token_hash` جدول `user_sessions` نگهداری می‌شود.
  * **اعتبارسنجی بلادرنگ سمت سرور (Immediate Server-side Revocation)**: در هر درخواست احرازهویت‌شده، علاوه بر اعتبارسنجی امضای JWT، وضعیت نشست متناظر با `session_id` در دیتابیس بررسی می‌شود (نشست وجود داشته باشد، `revoked_at` تهی باشد، منقضی نشده باشد و کاربر فعال باشد). در زمان Logout، تغییر رمز عبور، یا مسدودسازی کاربر، نشست‌های مربوطه در دیتابیس Revoke می‌شوند و پس از آن کلیه Access Tokenهای قدیمی حتی قبل از اتمام TTL پانزده دقیقه‌ای نامعتبر شناخته می‌شوند.
* **Rationale**: تفکیک Access Token کوتاه‌مدت از Session Token بلندمدت در کوکی `HttpOnly` همراه با پایداری وضعیت نشست‌ها در دیتابیس، بالاترین ضریب ایمنی را در برابر سرقت توکن، حملات XSS/CSRF و سناریوهای ابطال فوری فراهم می‌سازد.
* **Consequences / Trade-offs**: نیاز به کوئری اعتبارسنجی وضعیت نشست در درخواست‌های وب (که با ایندکس‌های بهینه روی `user_sessions` و Caching سبک در لایه سرویس کم‌هزینه است).
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md), [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md), [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md)

---

## ADR-007 — نحوه ذخیره‌سازی کلمات عبور کاربران (Password Storage)

* **Status**: `Accepted`
* **Context**: کلمات عبور کاربران سیستم باید به گونه‌ای ذخیره شوند که حتی در صورت افشای پایگاه داده، غیرقابل بازیابی باشند.
* **Decision**: کلمات عبور هرگز رمزگذاری (Encrypt) نمی‌شوند، بلکه با الگوریتم‌های استاندارد یک‌طرفه مقاوم در برابر سخت‌افزارهای موازی هش می‌شوند. الگوریتم ترجیحی **`Argon2id`** بوده و **`bcrypt`** با Cost مناسب به عنوان فال‌بک استاندارد پذیرفته است.
* **Rationale**:
  * انطباق با بالاترین استانداردهای امنیتی OWASP و NIST.
  * مقاومت بالا در برابر حملات Brute-force و GPU/ASIC Cracking.
* **Consequences / Trade-offs**: مصرف اندک منابع پردازشی و رم در زمان محاسبه هش لاگین (که با Rate Limiting مهار می‌شود).
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)

---

## ADR-008 — رمزنگاری گواهی‌های دسترسی خارجی در حالت سکون (External Credential Encryption)

* **Status**: `Accepted`
* **Context**: گواهی‌های اتصال به سرورهای مشتریان (کلیدهای SSH، پسوردها و توکن‌های API) باید بر خلاف پسورد کاربر، در زمان اجرای بکاپ به صورت متنی قابل بازیابی باشند اما در دیتابیس کاملاً رمزشده ذخیره گردند.
* **Decision**: کلیه سکرت‌های موجودیت `credentials` با الگوریتم **`AES-256-GCM`** (رمزنگاری احرازهویت‌شده) رمزگذاری می‌شوند. کلید اصلی (`Master Key`) خارج از کد منبع و مخزن گیت از طریق متغیر محیطی تزریق می‌شود. کار با سکرت‌ها از اصول حداقل طول عمر (`minimum secret lifetime`)، دامنه محدود به تابع (`function-scoped usage`) و پاک‌سازی بهینه حافظه (`best-effort zeroization`) پیروی می‌کند. این اقدامات برای کاهش مدت‌زمان و سطح مواجهه (Exposure) سکرت در حافظه رم به کار می‌روند؛ با این حال، پاک‌شدن کامل و تضمین‌شده تمامی کپی‌های احتمالی حافظه به دلیل رفتار درونی Garbage Collection در Runtime زبان Go ادعا نمی‌شود.
* **Rationale**:
  * تضمین محرمانگی و اصالت داده‌ها و جلوگیری از دستکاری Ciphertext.
  * امکان چرخش کلید از طریق متادیتای `key_version` و متادیتای اثرانگشت (`fingerprint metadata`).
  * به حداقل رساندن سطح ریسک و مواجهه سکرت‌ها در حافظه پروسس Go.
* **Consequences / Trade-offs**: وابستگی کامل بالا آمدن سرویس به حضور `ENCRYPTION_MASTER_KEY` در محیط اجرا.
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md), [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md)

---

## ADR-009 — تفکیک منبع، اتصال‌دهنده و گواهی دسترسی (Resource & Connector Model)

* **Status**: `Accepted`
* **Context**: ساختار اتصال به سرورها نیازمند تفکیک شفاف میان تعریف موجودیت منبع، اطلاعات اتصال و گواهی‌های دسترسی جهت قابلیت توسعه و چرخش امن کردانشال‌ها است.
* **Decision**:
  * در نسخه ۱، هر `Resource` دارای یک پیکربندی اتصال متناظر به صورت رابطه یک‌به‌یک (**`Resource 1:1 ResourceConnector`**) است.
  * هر `ResourceConnector` به یک `Credential` ارجاع می‌دهد (`credential_id`).
  * یک `Credential` در صورت مجاز بودن سیاست سیستم می‌تواند توسط چند Connector/Resource مورد استفاده قرار گیرد.
  * انواع کانونی منابع در V1 برابر با **`ubuntu_ssh`** و **`cpanel`** هستند.
  * معماری اتصال‌دهنده‌ها مبتنی بر قابلیت‌ها (`Capability-based Interface`) است. احراز هویت با کلید SSH بر پسورد و توکن cPanel بر پسورد اکانت ترجیح دارد و بررسی اثرانگشت میزبان (`SSH Host Key Verification`) الزامی است.
  * *تأکید معماری*: جداسازی `Resource`، `ResourceConnector` و `Credential` صرفاً جهت قابلیت توسعه‌پذیری (Extensibility) و چرخش آسان کردانشال‌ها (Credential Rotation) انجام شده است و به معنی ایجاد معماری چندبه‌چند (Many-to-Many) میان منبع و کانکتور در V1 نیست.
* **Rationale**:
  * امکان چرخش کلیدها و پسوردها در جدول `credentials` بدون نیاز به دستکاری متادیتای منبع.
  * سادگی در افزودن پشتیبانی از DirectAdmin، Plesk یا Windows Agent در آینده بر پایه همان واسط عمومی.
* **Consequences / Trade-offs**: نیاز به Join میان جداول مربوطه در زمان واکشی اطلاعات اتصال.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md)

---

## ADR-010 — مدل چندلایه‌ای چرخه حیات پشتیبان‌گیری (Backup Lifecycle Model)

* **Status**: `Accepted`
* **Context**: فرآیند پشتیبان‌گیری دارای ابعاد سیاست‌گذاری، زمان‌بندی، درخواست‌های اجرایی و فایل‌های فیزیکی خروجی است و ترکیب این مفاهیم باعث خرابی تاریخچه ممیزی می‌شود.
* **Decision**: چرخه حیات بکاپ به ۴ لایه تفکیک شد:
  `BackupPlan` (سیاست و زمان‌بندی) ➔ `BackupJob` (درخواست منطقی) ➔ `BackupRun` (تلاش اجرایی واقعی) ➔ `BackupArtifact` (خروجی فیزیکی تولیدشده).
  تلاش‌های مجدد (Retry) یک `BackupRun` جدید با `attempt_number` افزایشی می‌سازند و رکوردهای قبلی هرگز حذف یا بازنویسی نمی‌شوند.
* **Rationale**:
  * حفظ شفاف تاریخچه تمام تلاش‌ها برای عیب‌یابی و ممیزی.
  * پشتیبانی یکپارچه از جاب‌های دستی مستقل بدون نیاز به تعریف Plan.
* **Consequences / Trade-offs**: افزایش تعداد رکوردهای پایگاه داده در طول زمان (که با کوئری‌های صفحه‌بندی‌شده مهار می‌شود).
* **Related Documents**: [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md), [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-011 — مدل وضعیت‌های مجاز جاب و ران (Job & Run Status Model)

* **Status**: `Accepted`
* **Context**: ماشین وضعیت پردازش وظایف باید در تمام اسناد، APIها و لایه دیتابیس کاملاً یکدست و بدون ابهام باشد.
* **Decision**: وضعیت‌های مجاز و کانونی به شرح زیر تثبیت شدند:
  * وضعیت‌های **`BackupJob`**: منحصراً `pending`, `running`, `completed`, `failed`, `cancelled`.
  * وضعیت‌های **`BackupRun`**: منحصراً `pending`, `running`, `success`, `failed`, `cancelled`.
  * در نسخه ۱، لغو دستی فقط قبل از شروع اجرا مجاز است (`BackupJob: pending ➔ cancelled`) و لغو Run فعال پیاده‌سازی نمی‌شود.
* **Rationale**:
  * تفکیک دقیق وضعیت نهایی درخواست منطقی (`completed`) از موفقیت تلاش فیزیکی (`success`).
  * جلوگیری از ناهماهنگی در قطع نشست‌های استریمینگ شبکه در نسخه ۱.
* **Consequences / Trade-offs**: جاب‌هایی که کارگر اجرای آن‌ها را آغاز کرده تا پایان فرآیند یا بروز خطا قابل لغو آنی نیستند.
* **Related Documents**: [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md), [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-012 — صف وظایف پایدار در پایگاه داده (Durable Queue Strategy)

* **Status**: `Accepted`
* **Context**: سیستم نیازمند صف وظایفی است که با ریستارت شدن سرور، وظایف در صف از دست نروند و هم‌زمان نیاز به زیرساخت‌های سنگین خارجی نداشته باشد.
* **Decision**: رکوردهای جدول `backup_jobs` با وضعیت `pending` در پایگاه داده PostgreSQL نقش صف پایدار (`Durable Queue`) را ایفا می‌کنند. کارگرها جاب‌ها را به صورت اتمیک و تراکنشی تصاحب (`Transactional Claim`) می‌کنند. کانال‌های درون‌حافظه‌ای Go منبع اصلی حقیقت نیستند.
* **Rationale**:
  * عدم اتکا به سرویس‌های جانبی نظیر Kafka یا RabbitMQ در نسخه ۱.
  * پایداری کامل صف در برابر قطعی برق و کرش برنامه.
* **Consequences / Trade-offs**: وابستگی کارایی صف به سرعت تراکنش‌های دیتابیس (که برای بار کاری V1 کاملاً کافی و بهینه است).
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-013 — معماری کارگرهای پردازش پس‌زمینه (Worker Architecture)

* **Status**: `Accepted`
* **Context**: اجرای پشتیبان‌گیری باید در پس‌زمینه بدون مسدودسازی درخواست‌های وب و با مدیریت ایمن منابع سرور انجام شود.
* **Decision**: نسخه ۱ از Pool کارگرهای داخلی مبتنی بر Goroutine در همان فرآیند Go استفاده می‌کند. کارگرها کاملاً از لایه HTTP و احراز هویت وب تفکیک شده و منحصراً از طریق Service Layer عمل می‌کنند. سیستم مجهز به مکانیزم‌های Graceful Shutdown و Startup Recovery است.
* **Rationale**:
  * سادگی استقرار و مدیریت چرخه حیات پردازش‌ها در معماری Modular Monolith.
  * مصرف بهینه پردازنده و رم نسبت به فرآیندهای مجزای سیستم‌عامل.
* **Consequences / Trade-offs**: در صورت کرش باینری اصلی، کلیه کارگرها متوقف می‌شوند که توسط مکانیزم Startup Recovery بازیابی خواهند شد.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-014 — کنترل همروندی و قفل‌گذاری منابع (Resource Concurrency & Locking)

* **Status**: `Accepted`
* **Context**: اجرای هم‌زمان دو عملیات بکاپ سنگین روی یک سرور مقصد باعث افت شدید کارایی، قطعی سرویس مشتری یا تداخل در داده‌ها می‌شود.
* **Decision**: در نسخه ۱، کنترل همروندی روی هر منبع از طریق **`Per-Resource Mutex`** در حافظه برنامه Go اعمال می‌شود. فیلدهای `heartbeat_at` و `lease_until` روی موجودیت `BackupRun` صرفاً برای رهگیری مالکیت و کشف کرش کارگر (`Stale Run Detection`) استفاده می‌شوند. قفل توزیع‌شده در دیتابیس صرفاً مسیر توسعه آینده برای Multiple Worker Nodes خواهد بود.
* **Rationale**:
  * بالاترین سرعت و سادگی در کنترل همروندی در نسخه تک‌فرآیندی V1.
  * جلوگیری قطعی از اضافه بار روی سرورهای ابونتو و هاست‌های اشتراکی.
* **Consequences / Trade-offs**: قفل‌های Mutex درون‌حافظه‌ای با ریستارت برنامه ریست می‌شوند که با چک کردن وضعیت در زمان بوت جبران می‌گردد.
* **Related Documents**: [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-015 — استراتژی تلاش مجدد در خطاها (Retry Strategy)

* **Status**: `Accepted`
* **Context**: نوسانات موقت شبکه نباید منجر به شکست دائمی بکاپ‌ها شود، اما خطاهای قطعی نباید منابع سرور را بیهوده هدر دهند.
* **Decision**: تلاش مجدد منحصراً برای خطاهای گذرا و موقت شبکه و I/O با الگوریتم وقفه نمایی فزاینده (`Exponential Backoff with Jitter`) اعمال می‌شود. خطاهای احراز هویت، اعتبارسنجی و پیکربندی بدون تلاش مجدد متوقف می‌گردند. هر تلاش یک `BackupRun` جدید می‌سازد و پارامتر `max_retry` یک سیاست در سطح برنامه است (بدون ستون اضافی در دیتابیس).
* **Rationale**:
  * تاب‌آوری در برابر قطع لحظه‌ای ارتباط SSH بدون ایجاد حلقه تکرار در مواجهه با پسورد اشتباه.
  * حفظ سادگی مدل داده دیتابیس و مشتق‌سازی تلاش‌ها از تاریخچه.
* **Consequences / Trade-offs**: نیاز به تعریف دقیق دسته‌بندی خطاها (Retryable vs Non-retryable) در لایه درایورها.
* **Related Documents**: [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-016 — مسئولیت و رفتار زمان‌بند (Scheduler Responsibility)

* **Status**: `Accepted`
* **Context**: سیستم زمان‌بندی باید برنامه‌های سررسیدشده را دقیقاً مدیریت کند بدون اینکه دچار گره یا از دست رفتن وظایف شود.
* **Decision**: کامپوننت `Scheduler` صرفاً وضعیت برنامه‌های فعال را ارزیابی کرده و در زمان سررسید یک `BackupJob` در وضعیت `pending` ایجاد می‌کند. زمان‌بند **هرگز مستقیماً بکاپ اجرا نمی‌کند**. در صورت مشغول بودن منبع، جاب حذف یا Skip نمی‌شود و در وضعیت `pending` منتظر آزاد شدن منبع باقی می‌ماند. از تولید جاب‌های تکراری مکرر جلوگیری می‌گردد.
* **Rationale**:
  * تفکیک صریح وظایف (Single Responsibility Principle).
  * تضمین اینکه بارهای کاری سنگین مانع از اجرای دقیق تیکر زمان‌بندی نشوند.
* **Consequences / Trade-offs**: در صورت اشغال طولانی‌مدت یک منبع، جاب‌ها در صف دیتابیس انباشته می‌شوند تا به نوبت اجرا گردند.
* **Related Documents**: [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-017 — انتزاع و راهبرد موتور پشتیبان‌گیری (Backup Engine Strategy)

* **Status**: `Accepted`
* **Context**: نحوه استخراج داده‌ها بر اساس نوع منبع و نیازهای آینده به بهینه‌سازی حجم ذخیره‌سازی باید کاملاً مستقل از لایه‌های وب و اتصال باشد.
* **Decision**: مفهوم **`BackupEngine`** به عنوان یک انتزاع مستقل تعریف شد. در نسخه ۱ از **`Direct Stream Backup Engine`** استفاده می‌شود که عملیات `MySQL Dump` و `Website Files Archive` را به صورت استریم پیوسته با فشرده‌ساز `gzip` ترکیب می‌کند. در آینده موتورهایی نظیر **`restic`** برای پشتیبان‌گیری افزایشی افزوده خواهند شد.
* **Rationale**:
  * امکان تعویض یا ارتقای موتور بکاپ بدون تغییر در درایورهای کانکتور یا لایه وب.
  * حداقل مصرف دیسک موقت در نسخه ۱ با هدایت مستقیم استریم‌ها به فشرده‌ساز.
* **Consequences / Trade-offs**: در نسخه ۱، بکاپ‌ها Full هستند و فشرده‌سازی افزایشی (Incremental) به فازهای آتی موکول شده است.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md)

---

## ADR-018 — انتزاع و راهبرد لایه ذخیره‌سازی (Storage Strategy)

* **Status**: `Accepted`
* **Context**: مقصد ذخیره فایل‌های پشتیبان باید به گونه‌ای انتزاع یابد که تغییر از فایل‌سیستم محلی به ذخیره‌سازی ابری مستلزم تغییر در منطق تجاری نباشد.
* **Decision**: مفهوم **`StorageProvider`** به صورت انتزاعی کاملاً مستقل از `BackupEngine` پیاده‌سازی شد. در نسخه ۱ از **`Local Storage Provider`** در مسیر `/srv/backup-platform/artifacts/` استفاده می‌شود و در آینده ارائه‌دهنده‌های ابری سازگار با S3 افزوده خواهند شد. مسیرهای فیزیکی فایل‌سیستم هرگز در API افشا نمی‌شوند.
* **Rationale**:
  * رعایت اصل Open/Closed برای افزودن مقاصد ذخیره‌سازی ابری در فازهای بعدی.
  * ایزولاسیون کامل دایرکتوری‌های حساس سرور از دید کاربران.
* **Consequences / Trade-offs**: محدودیت حجم ذخیره‌سازی V1 به ظرفیت دیسک محلی سرور میزبان.
* **Related Documents**: [docs/ARCHITECTURE.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ARCHITECTURE.md), [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)

---

## ADR-019 — امنیت و مجوزهای فایل‌سیستم محلی (Local Filesystem Security)

* **Status**: `Accepted`
* **Context**: سرور میزبان با دو وب‌سایت کم‌ترافیک دیگر به اشتراک گذاشته شده است؛ فایل‌های بکاپ حاوی کل داده‌های دیتابیس مشتریان هستند و باید از دسترسی سایر پروسس‌ها محافظت شوند.
* **Decision**: دایرکتوری‌های ذخیره‌سازی با دسترسی اکید **`0700`** و فایل‌های آرتیفکت با دسترسی **`0600`** ایجاد می‌شوند. برنامه تحت یک کاربر سرویس اختصاصی بدون دسترسی روت (`non-root`) اجرا شده و محل ذخیره بکاپ‌ها کاملاً خارج از روت وب عمومی قرار دارد.
* **Rationale**:
  * جلوگیری از خوانده شدن فایل‌های بکاپ توسط وب‌سرورهای همسایه، کاربران سیستمی دیگر یا پروسس‌های اشتراکی.
* **Consequences / Trade-offs**: نیاز به تنظیم دقیق مالکیت دایرکتوری‌ها (`chown`) در فرآیند راه‌اندازی و داکر.
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md)

---

## ADR-020 — وضعیت رمزگذاری آرتیفکت‌ها در حالت سکون (Artifact Encryption at Rest)

* **Status**: `Accepted`
* **Context**: مشتریان نیازمند شفافیت در خصوص سطح حفاظت از داده‌های بکاپ روی دیسک هستند.
* **Decision**: در نسخه داخلی ۱ با موتور Direct Stream، آرتیفکت‌ها بر روی دیسک محلی ممکن است در سطح داده رمزگذاری در سکون نداشته باشند و امنیت آن‌ها بر عهده مجوزهای فایل‌سیستم لینوکس است. این محدودیت به صورت شفاف شناخته می‌شود؛ اما قبل از راه‌اندازی **Public Commercial SaaS**، پیاده‌سازی **Encryption at Rest** برای کلیه آرتیفکت‌های بکاپ یک الزام تولیدی قطعی (`Production Requirement`) است و موتورهایی نظیر `restic` به عنوان **یکی از گزینه‌های آینده برای پیاده‌سازی** (بدون انحصار ابزار) مد نظر خواهند بود.
* **Rationale**:
  * سادگی و سرعت توسعه نسخه داخلی V1 بدون ایجاد سربار رمزنگاری لایه فایل در فاز اول.
  * تعیین مرز شفاف امنیتی پیش از ورود به محیط تجاری ابری چندمستأجری.
* **Consequences / Trade-offs**: امنیت فیزیکی داده‌ها در V1 کاملاً وابسته به امنیت هاست و ایزولاسیون سیستم‌عامل سرور میزبان است.
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-021 — استراتژی اعتبارسنجی سلامت فایل‌های پشتیبان (Verification Strategy)

* **Status**: `Accepted`
* **Context**: صرف تولید یک فایل روی دیسک دلیلی بر سالم بودن فرآیند پشتیبان‌گیری نیست؛ فایل‌های ناقص یا خراب می‌توانند در زمان بازیابی موجب شکست عملیات شوند.
* **Decision**: هیچ بکاپی بدون عبور موفق از پایپ‌لاین اعتبارسنجی وضعیت `success` دریافت نمی‌کند. اعتبارسنجی در نسخه ۱ منحصراً شامل موارد زیر است:
  1. سایز آرتیفکت بزرگتر از صفر (`artifact size > 0`).
  2. صحت و تطبیق کامل هش `SHA-256` (`SHA-256 integrity`).
  3. سلامت ساختاری فایل فشرده (`compressed archive structural integrity` شامل `gzip -t` برای دامپ دیتابیس و `tar -tzf` برای آرشیو فایل‌ها).
  4. اعتبارسنجی‌های پایه ساختاری و فرمت (`basic format / sanity checks`).
  *تصریح قطعی*: این آزمون‌ها صرفاً سلامت فیزیکی و ساختار آرشیو را تایید می‌کنند و به معنی تضمین کامل بودن منطقی داده‌ها یا تضمین صددرصدی موفقیت بازیابی (Restore) نیستند. تست کامل Restore جزو تعهدات V1 نیست و در فازهای آتی پیگیری خواهد شد.
* **Rationale**:
  * اطمینان از سلامت فیزیکی و ساختاری فایل آرتیفکت با کمترین مصرف منابع پردازنده و رم بلافاصله پس از تولید.
* **Consequences / Trade-offs**: اضافه شدن چند ثانیه به زمان پایان هر Run جهت اجرای تست سلامت ساختار.
* **Related Documents**: [docs/WORKER_EXECUTION_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/WORKER_EXECUTION_DESIGN.md), [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md)

---

## ADR-022 — ثبت وقایع حسابرسی و ممیزی (Audit Logging Strategy)

* **Status**: `Accepted`
* **Context**: پلتفرم نیازمند نگهداری یک تاریخچه حسابرسی قابل ردیابی و فقط-افزودنی (`traceable and append-oriented audit history`) برای عملیات حساس کاربران است، بدون اینکه لاگ‌ها با رویدادهای روتین پس‌زمینه پر شوند.
* **Decision**: جدول `audit_logs` به صورت ساختار فقط-افزودنی (`Append-oriented`) در پایگاه داده ایجاد می‌شود. کلیه عملیات ایجاد، ویرایش، حذف، دانلود و تغییرات حساس که توسط کاربران آغاز می‌شوند لاگ می‌گردند. استفاده داخلی کارگرها از سکرت‌ها برای جاب‌های دوره‌ای باعث ایجاد رویدادهای بی‌رویه `credential.access` نخواهد شد.
  *تصریح قطعی*: رویکرد فقط-افزودنی در دیتابیس به معنای ذخیره‌سازی تغییرناپذیر سخت‌افزاری یا WORM نیست (`Append-oriented != WORM / Immutable`) و سیستم در نسخه ۱ ادعای عدم انکار رمزنگاری‌شده (`cryptographic non-repudiation`) ندارد.
* **Rationale**:
  * ایجاد تاریخچه شفاف و قابل ردیابی از رفتارهای کاربران ادمین و اعضا.
  * جلوگیری از متورم شدن غیرضروری حجم پایگاه داده توسط فرآیندهای پس‌زمینه.
* **Consequences / Trade-offs**: نیاز به اعمال سیاست‌های آرشیو و نگهداری دوره‌ای برای جدول Audit Logs در نسخه‌های بعد.
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md), [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md)

---

## ADR-023 — سیاست حفظ تاریخچه و حذف منطقی (Soft Delete / History Preservation)

* **Status**: `Accepted`
* **Context**: حذف فیزیکی داده‌های پایه در سیستم مدیریت بکاپ باعث ایجاد رکوردهای یتیم (Orphan) و مخدوش شدن گزارش‌های حسابرسی و مالی می‌شود.
* **Decision**: در نسخه ۱، موجودیت‌های `organizations`, `resources` و `backup_plans` حذف فیزیکی نمی‌شوند بلکه آرشیو (`status = 'archived'`) می‌گردند. در V1، سیاست اعمال نگهداری آرتیفکت‌ها (Artifact Retention) یا حذف فیزیکی `BackupArtifact` باعث حذف تاریخچه `BackupJob` یا `BackupRun` نمی‌شود و این سوابق برای مقاصد ممیزی (Audit) و تاریخچه عملیاتی (Operational History) حفظ می‌گردند. در صورتی که در آینده سیاست چرخه حیات (Lifecycle) جداگانه‌ای برای متادیتا و تاریخچه تعریف شود، باید با ADR جدید تصویب گردد.
* **Rationale**:
  * حفظ ارتباط یکپارچه لاگ‌های حسابرسی و سوابق تلاش‌های بکاپ.
  * جلوگیری از دست رفتن متادیتا در صورت خطای سهوی کاربر.
* **Consequences / Trade-offs**: لزوم فیلتر کردن رکوردهای آرشیوشده در لیست‌های پیش‌فرض UI/API.
* **Related Documents**: [docs/DATA_MODEL.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/DATA_MODEL.md), [docs/API_DESIGN.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/API_DESIGN.md)

---

## ADR-024 — امنیت شبکه و دسترسی در نسخه داخلی ۱ (Internal V1 Network Security)

* **Status**: `Accepted`
* **Context**: نسخه ۱ ممکن است در ابتدا روی IP سرور و بدون دامنه یا گواهی TLS عمومی راه‌اندازی شود؛ انتقال سکرت‌ها روی اینترنت باز خطرات جدی دارد.
* **Decision**: تا پیش از راه‌اندازی رسمی Production TLS، لاگین و ارسال اطلاعات هویتی نباید از اینترنت عمومی روی بستر HTTP خام صورت گیرد. دسترسی موقت مدیریتی در V1 صرفاً از طریق شبکه خصوصی (`Private Network`)، **VPN** یا **SSH Tunnel** به سرور میزبان مجاز است. راه‌اندازی نسخه عمومی Commercial SaaS بدون Production TLS اکیداً ممنوع است.
* **Rationale**:
  * پیشگیری قطعی از حملات شنود (Eavesdropping) و Man-in-the-Middle روی پسوردها و توکن‌ها.
* **Consequences / Trade-offs**: نیاز ادمین‌ها به استفاده از VPN یا تونل SSH در فاز آزمایشی اولیه.
* **Related Documents**: [docs/SECURITY.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SECURITY.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-025 — راهبرد استقرار نسخه ۱ (Deployment Strategy)

* **Status**: `Accepted`
* **Context**: فرآیند استقرار باید پایدار، قابل تکرار و ایزوله از دو وب‌سایت دیگر موجود روی سرور باشد.
* **Decision**: استقرار نسخه ۱ به صورت تک‌نودی با **Docker Compose** انجام می‌شود. سرویس باینری Go و دیتابیس PostgreSQL با Volumeهای پایدار مجزا اجرا می‌شوند. این تصمیم به معنی پیاده‌سازی Containerized Microservices نیست بلکه استقرار کانتینری یکپارچه برای Modular Monolith است.
* **Rationale**:
  * ایزولاسیون کامل وابستگی‌های محیطی و نسخه‌های کتابخانه‌های سیستمی از سایر سایت‌های هاست.
  * سادگی در راه‌اندازی، پشتیبان‌گیری و به‌روزرسانی با یک دستور.
* **Consequences / Trade-offs**: لزوم تنظیم دقیق مپینگ دایرکتوری‌های محلی و مجوزهای فایل داکر.
* **Related Documents**: [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-026 — استراتژی اولویت تحویل: تمرکز نخست بر MySQL (MySQL First Delivery Strategy)

* **Status**: `Accepted`
* **Context**: توسعه باید مبتنی بر برش‌های عمودی با ارزش بالا باشد تا از پیچیدگی هم‌زمان چند ماژول جلوگیری شود.
* **Decision**: اولین برش عمودی کامل (`Vertical Slice`) پلتفرم منحصراً روی پشتیبان‌گیری دستی پایگاه داده MySQL در منبع `ubuntu_ssh` پیاده‌سازی و اعتبارسنجی می‌شود. پس از پایداری، پشتیبانی از MySQL در هاست `cpanel` اضافه شده و سپس قابلیت پشتیبان‌گیری از فایل‌های وب‌سایت پیاده‌سازی خواهد شد.
* **Rationale**:
  * حل چالش‌های اصلی صف، قفل همروندی، رمزنگاری و اعتبارسنجی در یک سناریوی واقعی و پرکاربرد قبل از توسعه سایر فیچرها.
* **Consequences / Trade-offs**: به تعویق افتادن قابلیت پشتیبان‌گیری از فایل‌ها تا پس از تثبیت کامل پایپ‌لاین دیتابیس.
* **Related Documents**: [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-027 — راهبرد آینده پشتیبان‌گیری MSSQL و محیط ویندوز (MSSQL / Windows Future Strategy)

* **Status**: `Deferred`
* **Context**: در آینده پشتیبانی از پایگاه داده‌های Microsoft SQL Server و نرم‌افزارهای مالی ویندوزی (سپیدار، هلو) مورد نیاز خواهد بود.
* **Decision**: برای این منابع در آینده یک عامل سبک ویندوزی (**`Windows Backup Agent`**) توسعه خواهد یافت. عامل ویندوز فایل محلی استاندارد `.bak` ایجاد کرده و از طریق ارتباط امن HTTPS به پلتفرم ارسال می‌کند. پورت MSSQL (`1433`) هرگز نباید برای پلتفرم روی اینترنت عمومی باز شود.
* **Rationale**:
  * ایجاد پشتیبان سازگار با تراکنش‌های فعال بدون باز کردن پورت‌های خطرناک دیتابیس به روی اینترنت.
* **Consequences / Trade-offs**: نیازمندی به توسعه و نگهداری یک کدبیس مجزا برای Agent ویندوز در فازهای بعدی.
* **Related Documents**: [docs/SPECIFICATION.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/SPECIFICATION.md), [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-028 — تفکیک و تعویق قابلیت‌های تجاری ابری (Public SaaS Features Deferral)

* **Status**: `Deferred`
* **Context**: تحمیل ویژگی‌های پلتفرم‌های ابری بزرگ به نسخه اولیه باعث شکست پروژه در تحویل به موقع خواهد شد.
* **Decision**: ویژگی‌های زیر به طور رسمی به فازهای پس از تثبیت نسخه داخلی V1 موکول شدند:
  * ثبت‌نام عمومی مشتریان (`Public Customer Registration`).
  * درگاه پرداخت، صدور صورت‌حساب و مدیریت اشتراک‌ها (`Billing & Subscriptions`).
  * پورتال سلف‌سرویس و احراز هویت دومرحله‌ای (`MFA`).
  * سیستم دسترسی پیشرفته دانه‌ریز (Advanced RBAC) و Row Level Security (RLS) در دیتابیس.
  * کارگرهای توزیع‌شده و ذخیره‌سازی ابری در مقیاس بالا.
* **Rationale**:
  * تمرکز صددرصدی تیم بر ساخت یک موتور پشتیبان‌گیری بی‌نقص و مطمئن در هسته سیستم.
* **Consequences / Trade-offs**: عدم امکان استفاده عمومی تجاری از پلتفرم در نسخه ۱.
* **Related Documents**: [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-029 — انتخاب معکوس‌کننده پروکسی و مدیریت TLS (Reverse Proxy Choice)

* **Status**: `Pending`
* **Context**: برای استقرار نسخه پروداکشن با دامنه عمومی، نیازمند یک Reverse Proxy جهت مدیریت گواهی‌های SSL/TLS، پایان‌دهی HTTPS و هدایت ترافیک به کانتینر برنامه Go هستیم.
* **Decision**: دو گزینه **`Caddy`** (به دلیل مدیریت خودکار و بی‌دردسر گواهی‌های Let's Encrypt) و **`Nginx`** (به دلیل بلوغ و استاندارد صنعتی فراگیر) به عنوان گزینه‌های معتبر در نظر گرفته شده‌اند.
* **وضعیت تصمیم**: انتخاب نهایی بین Caddy و Nginx تا زمان فاز استقرار پروداکشن (Phase 10 / Public TLS Deployment) باز می‌ماند و این موضوع مسدودکننده (Blocker) آغاز پیاده‌سازی کدهای بک‌اند نیست.
* **Rationale**: عدم تحمیل وابستگی ابزار خارجی به لایه کدهای Go در مراحل اولیه توسعه.
* **Consequences / Trade-offs**: نهایی‌سازی فایل کانفیگ پروکسی در انتهای نقشه راه انجام خواهد شد.
* **Related Documents**: [docs/ROADMAP.md](file:///c:/Users/Kroos/Desktop/backup-platform/docs/ROADMAP.md)

---

## ADR-030 — نظام مدیریت تغییرات معماری (Architecture Change Control)

* **Status**: `Accepted`
* **Context**: با رشد کدبیس، تصمیمات معماری نباید به صورت سلیقه‌ای یا ضمنی درون کدهای منبع دچار انحراف شوند.
* **Decision**: اسناد تأییدشده مرجع انحصاری معماری هستند. هر تصمیم معماری جدید یا تغییر در تصمیمات با وضعیت `Accepted` باید ابتدا در قالب یک ADR جدید در این سند ثبت شده، اثرات آن بر سایر اسناد بررسی گردد و پس از تصویب وارد مرحله کدنویسی شود. کدها هرگز نباید منبع تصمیمات معماری پنهان باشند.
* **Rationale**:
  * حفظ شفافیت، یکپارچگی مهندسی و مستندسازی تصمیمات در کل چرخه حیات نرم‌افزار.
* **Consequences / Trade-offs**: الزام به انضباط تیمی در به‌روزرسانی مستندات پیش از اعمال تغییرات ساختاری در کد.
* **Related Documents**: کلیه اسناد موجود در دایرکتوری `docs/`.

---

## جدول خلاصه وضعیت کلیه تصمیمات معماری (ADR Summary Table)

| شناسه | عنوان تصمیم معماری | وضعیت (Status) | قلمرو (Scope) |
| :--- | :--- | :---: | :---: |
| **ADR-001** | انتخاب زبان برنامه‌نویسی سمت سرور (Go) | `Accepted` | V1 |
| **ADR-002** | معماری کلی برنامه (Modular Monolith) | `Accepted` | V1 |
| **ADR-003** | پایگاه داده متادیتا و صف وظایف (PostgreSQL) | `Accepted` | V1 |
| **ADR-004** | مدل چندمستأجری و ایزولاسیون سازمان‌ها | `Accepted` | V1 |
| **ADR-005** | راهبرد و استانداردهای طراحی رابط برنامه‌نویسی (REST API) | `Accepted` | V1 |
| **ADR-006** | راهبرد احراز هویت و مدیریت نشست‌ها (Session Revocation) | `Accepted` | V1 |
| **ADR-007** | نحوه ذخیره‌سازی کلمات عبور کاربران (Argon2id/bcrypt) | `Accepted` | V1 |
| **ADR-008** | رمزنگاری گواهی‌های دسترسی خارجی (AES-256-GCM) | `Accepted` | V1 |
| **ADR-009** | تفکیک منبع، اتصال‌دهنده و گواهی دسترسی (`ubuntu_ssh` / `cpanel`) | `Accepted` | V1 |
| **ADR-010** | مدل چندلایه‌ای چرخه حیات پشتیبان‌گیری (Plan/Job/Run/Artifact) | `Accepted` | V1 |
| **ADR-011** | مدل وضعیت‌های مجاز جاب و ران | `Accepted` | V1 |
| **ADR-012** | صف وظایف پایدار در پایگاه داده (Durable Queue) | `Accepted` | V1 |
| **ADR-013** | معماری کارگرهای پردازش پس‌زمینه (In-process Go Worker Pool) | `Accepted` | V1 |
| **ADR-014** | کنترل همروندی و قفل‌گذاری منابع (`Per-Resource Mutex`) | `Accepted` | V1 |
| **ADR-015** | استراتژی تلاش مجدد در خطاها (Exponential Backoff) | `Accepted` | V1 |
| **ADR-016** | مسئولیت و رفتار زمان‌بند (Durable Job Creation Only) | `Accepted` | V1 |
| **ADR-017** | انتزاع و راهبرد موتور پشتیبان‌گیری (Direct Stream / Restic) | `Accepted` | V1 / Future |
| **ADR-018** | انتزاع و راهبرد لایه ذخیره‌سازی (Local Storage / S3) | `Accepted` | V1 / Future |
| **ADR-019** | امنیت و مجوزهای فایل‌سیستم محلی (`0700` / `0600`) | `Accepted` | V1 |
| **ADR-020** | وضعیت رمزگذاری آرتیفکت‌ها در حالت سکون (At Rest) | `Accepted` | V1 / Future |
| **ADR-021** | استراتژی اعتبارسنجی سلامت فایل‌های پشتیبان | `Accepted` | V1 |
| **ADR-022** | ثبت وقایع حسابرسی و ممیزی (Append-oriented Audit) | `Accepted` | V1 |
| **ADR-023** | سیاست حفظ تاریخچه و حذف منطقی (Soft Delete) | `Accepted` | V1 |
| **ADR-024** | امنیت شبکه و دسترسی در نسخه داخلی ۱ (Private/VPN) | `Accepted` | V1 |
| **ADR-025** | راهبرد استقرار نسخه ۱ (Single-node Docker Compose) | `Accepted` | V1 |
| **ADR-026** | استراتژی اولویت تحویل: تمرکز نخست بر MySQL | `Accepted` | V1 |
| **ADR-027** | راهبرد آینده پشتیبان‌گیری MSSQL و محیط ویندوز | `Deferred` | Future |
| **ADR-028** | تفکیک و تعویق قابلیت‌های تجاری ابری (Commercial SaaS) | `Deferred` | Future |
| **ADR-029** | انتخاب معکوس‌کننده پروکسی و مدیریت TLS (Caddy vs Nginx) | `Pending` | V1 Deployment |
| **ADR-030** | نظام مدیریت تغییرات معماری (Architecture Change Control) | `Accepted` | V1 / Future |

---

## تصمیمات باز قبل از پیاده‌سازی (Open Decisions Before Implementation)

تنها یک موضوع در وضعیت باز (`Pending`) قرار دارد که نحوه تصمیم‌گیری آن به شرح زیر است:

### انتخاب معکوس‌کننده پروکسی (Reverse Proxy Choice — Caddy vs Nginx):
* **وضعیت**: غیرمسدودکننده برای کدنویسی و توسعه؛ تا فاز استقرار نهایی پروداکشن (**Phase 10**) باز می‌ماند.
* **شرح**: انتخاب میان Caddy و Nginx تاثیری بر کدهای هسته باینری Go ندارد و صرفاً در پیکربندی داکر استقرار پروداکشن و TLS لحاظ خواهد شد.
