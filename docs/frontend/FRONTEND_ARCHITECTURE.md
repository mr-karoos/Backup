# Frontend Architecture Specification
## Phase F0 — Architecture & Technology Design Freeze

**Status**: F0 REVIEW CANDIDATE — Awaiting External Approval
**Target Repository Directory**: `/web`
**Baseline Git Commit**: `59d90ae074385ba27e1f14f00f886af6803a76b3`
**Companion Documents**:
* [./UX_INFORMATION_ARCHITECTURE.md](./UX_INFORMATION_ARCHITECTURE.md)
* [./DESIGN_SYSTEM.md](./DESIGN_SYSTEM.md)
* [./API_PAGE_MATRIX.md](./API_PAGE_MATRIX.md)

---

## ۱. اصول و جهت‌گیری کلان معماری (Architecture Core Principles)

1. **جداسازی کامل کلاینت و سرور در یک مخزن واحد (Clean Monorepo Isolation)**:
   * کلیه کدهای فرانت‌اند در پوشه ریشه `/web` قرار می‌گیرند و هیچ‌گونه وابستگی به ماژول‌های Go یا ابزارهای بیلد بک‌اند ندارند.
   * بک‌اند Go به عنوان یک سرویس Headless REST API با پیشوند `/api/v1` به صورت مستقل عمل می‌کند.
2. **مرجعیت قطعی قراردادهای بک‌اند (Backend Authority & Source-First Truth)**:
   * فرانت‌اند صرفاً مصرف‌کننده قراردادهای رسمی تأییدشده در اسناد معماری (`docs/API_DESIGN.md`, `docs/DATA_MODEL.md`) و هندلرهای واقعی Go است.
   * در صورت نیاز به قابلیت‌هایی که در حال حاضر فاقد Endpoint در بک‌اند هستند، فرانت‌اند اقدام به اختراع یا جعل Endpoint نکرده و آن‌ها را به صورت رسمی با برچسب **`BACKEND GAP`** علامت‌گذاری می‌کند.
3. **ایزولاسیون اکید مستأجران در سطح کلاینت (Strict Tenant Cache Isolation)**:
   * کلیه کلیدهای کش (Query Keys) در لایه سرور استیت شامل شناسه سازمان فعال (`organization_id`) هستند تا هرگز نشت داده میان سازمان‌ها رخ ندهد.
4. **امنیت داده‌ها و سکرت‌ها از مبدأ (Zero Plaintext Exposure & Write-Only Secrets)**:
   * هیچ‌گونه پسورد، کلید خصوصی، یا توکن اعتبارسنجی در حافظه محلی دائمی مرورگر (`localStorage`, `sessionStorage`) ذخیره نخواهد شد.
   * فیلدهای ورودی سکرت در فرم‌ها یک‌طرفه (Write-only) هستند و هرگز داده‌های سکرت از سرور بارگذاری مجدد یا در فیلدهای فرم بازخوانی نمی‌شوند.
5. **مرز شفاف رندرینگ سرور و کلاینت (RSC vs Authenticated Client Data Boundary)**:
   * توکن دسترسی (`access_token`) منحصراً در حافظه رم مرورگر نگهداری می‌شود و کامپوننت‌های سروری React (RSC) به آن دسترسی ندارند.
   * کلیه داده‌های عملیاتی احرازهویت‌شده منحصراً در مرز کلاینت و از طریق کلاینت متمرکز API و TanStack Query واکشی می‌شوند.
6. **ارتباطات تحت مبدأ یکپارچه (Same-Origin Deployment)**:
   * معماری استقرار فرانت‌اند به نحوی منجمد شده است که هم در توسعه محلی و هم در محیط پروداکشن، تمام درخواست‌ها به صورت Same-Origin ارسال گردند تا پیچیدگی‌ها و ریسک‌های امنیتی CORS حذف شوند.

---

## ۲. پشته فناوری فرانت‌اند (Frozen Frontend Stack)

پس از بررسی نیازمندی‌های سیستم، ساختار چندمستأجری و قراردادهای موجود، پشته فناوری زیر برای فرانت‌اند منجمد می‌گردد:

| لایه / بخش | انتخاب فنی منجمد | دلایل و منطق انتخاب (Rationale) |
| :--- | :--- | :--- |
| **Core Framework** | **Next.js 16.x — Active LTS line** | استاندارد قطعی صنعت برای وب‌اپلیکیشن‌های پیشرفته با پشتیبانی بومی از React Server Components (RSC)، سیستم مسیریابی توکار، بهینه‌سازی بارگذاری، و امکان پیاده‌سازی پروکسی امن در زمان توسعه. حداقل مرجع پچ‌شده مورد تایید: **Next.js 16.3.3**. خط اصلی و فرعی (16.x) منجمد بوده و در زمان پیاده‌سازی F1 از آخرین پچ امنیتی و پایدار استفاده می‌شود. |
| **UI Library** | **React 19.2.x** | هماهنگی کامل با Next.js 16، بهره‌گیری از هوک‌های جدید و مدیریت رندرینگ بهینه برای داشبوردهای عملیاتی سنگین. استفاده از آخرین نسخه پچ پایدار سازگار در فاز F1. |
| **Language** | **TypeScript 5.x (Strict Mode)** | انطباق ۱۰۰٪ با قراردادهای DTO بک‌اند از طریق تایپ‌های اکید، ممانعت از خطاهای زمان اجرا و تضمین یکپارچگی داده‌ها در سراسر برنامه. |
| **Styling** | **Tailwind CSS 4.3.x** | انجماد قطعی روی نسخه مدرن موتور CSS مبتنی بر کامپایلر Lightning CSS، کلاس‌های ابزاری مقیاس‌پذیر، عدم بار پردازشی در زمان اجرا (Zero Runtime CSS-in-JS)، و تطابق کامل با متغیرهای استاندارد CSS برای تم تاریک/روشن. |
| **Component Primitives** | **shadcn/ui + Radix UI Primitives** | کامپوننت‌های دسترسی‌پذیر (Accessible) منطبق با WCAG 2.2 AA، فاقد وابستگی‌های بسته‌بندی‌شده سنگین، با کنترل ۱۰۰٪ بر روی کدهای کامپوننت و سفارشی‌سازی بر اساس سیستم طراحی اختصاصی. |
| **Icons** | **Lucide React** | مجموعه آیکون‌های استاندارد، تمیز و یکنواخت برای محیط‌های زیرساختی و دواپس با حجم بسیار اندک و پشتیبانی عالی از Tree-shaking. |
| **Server State & Cache** | **TanStack Query v5 (React Query)** | استاندارد طلایی مدیریت داده‌های سرور، کش‌گذاری تفکیک‌شده بر پایه سازمان، ابطال هوشمند (Cache Invalidation)، مدیریت وضعیت‌های در حال بارگذاری/خطا، و استراتژی Polling بهینه برای نظارت بر جاب‌های در حال اجرا. |
| **Forms & Validation** | **React Hook Form + Zod** | مدیریت فرم‌ها به صورت Uncontrolled جهت حفظ بالاترین کارایی در صفحات پیچیده (مانند ویزارد بکاپ پلن)، اعتبارسنجی تطبیقی ۱۰۰٪ با قوانین DTO بک‌اند به کمک اسکیمای تایپ‌شده Zod و نمایش بلادرنگ خطاهای اعتبارسنجی. |
| **Lightweight Charts** | **Recharts (یا Tremor Primitives)** | مصورسازی سبک و هدفمند روندهای حجم بکاپ و زمان اجرای ران‌ها بدون تحمیل کتابخانه‌های گرافیکی حجیم و سنگین. |

> [!IMPORTANT]
> **سیاست نسخه‌گذاری و عدم نصب وابستگی‌ها در فاز F0**: خطوط اصلی و فرعی معماری (Major/Minor) به طور کامل منجمد هستند در حالی که پچ‌های امنیتی همواره به‌روز نگه‌داشته می‌شوند. در فاز F0 هیچ فایل پکیجی (`package.json`, `pnpm-lock.yaml`, `node_modules`) ایجاد یا نصب نخواهد شد.

---

## ۳. ساختار پوشه‌بندی مخزن (Monorepo `/web` Directory Structure)

ساختار پیشنهادی منجمد برای استقرار فرانت‌اند در مسیر `/web` به شرح زیر طراحی شده است:

```text
/
├── cmd/
│   └── server/                # باینری اصلی بک‌اند Go
├── docs/
│   ├── frontend/              # مستندات منجمد معماری و UX فرانت‌اند (F0)
│   │   ├── FRONTEND_ARCHITECTURE.md
│   │   ├── UX_INFORMATION_ARCHITECTURE.md
│   │   ├── DESIGN_SYSTEM.md
│   │   └── API_PAGE_MATRIX.md
│   └── ...                    # سایر اسناد معماری پروژه
├── internal/                  # ماژول‌های داخلی بک‌اند Go
└── web/                       # ریشه اختصاصی فرانت‌اند (آغاز از فاز F1)
    ├── app/                   # Next.js App Router صفحات و لایه‌بندی‌ها
    │   ├── (auth)/            # مسیرهای خارج از سشن لاگین
    │   │   ├── layout.tsx
    │   │   └── login/
    │   │       └── page.tsx
    │   ├── (dashboard)/       # شل اصلی اپلیکیشن لاگین‌شده با کانتکست سازمان
    │   │   ├── layout.tsx     # سایدبار، هدر، انتخاب سازمان و منوی کاربر
    │   │   ├── page.tsx       # صفحه داشبورد اصلی
    │   │   ├── resources/     # مدیریت منابع سروری
    │   │   │   ├── page.tsx
    │   │   │   ├── [id]/
    │   │   │   │   └── page.tsx
    │   │   │   └── new/
    │   │   │       └── page.tsx
    │   │   ├── plans/         # برنامه‌های پشتیبان‌گیری
    │   │   │   ├── page.tsx
    │   │   │   ├── [id]/
    │   │   │   │   └── page.tsx
    │   │   │   └── new/       # ویزارد چندمرحله‌ای ایجاد Plan
    │   │   │       └── page.tsx
    │   │   ├── runs/          # تاریخچه اجراها و رصد وضعیت جاب‌ها
    │   │   │   ├── page.tsx
    │   │   │   └── [id]/
    │   │   │       └── page.tsx
    │   │   ├── artifacts/     # لیست آرتیفکت‌ها، اعتبارسنجی و دانلود
    │   │   │   └── page.tsx
    │   │   ├── storage/       # مقاصد ذخیره‌سازی محلی و S3
    │   │   │   └── page.tsx
    │   │   ├── credentials/   # صندوقچه گواهی‌ها و کلیدها
    │   │   │   └── page.tsx
    │   │   ├── settings/      # تنظیمات سازمان
    │   │   │   └── page.tsx
    │   │   ├── health/        # مانیتورینگ سلامت سرویس (/api/v1/health)
    │   │   │   └── page.tsx
    │   │   └── platform-admin/# پنل ادمین پلتفرم (منحصراً برای is_system_admin)
    │   │       └── page.tsx
    │   ├── api/               # روت هندلرهای داخلی کلاینت (BFF/Proxy در صورت نیاز)
    │   ├── globals.css        # استایل‌های سراسری و توکن‌های CSS تم
    │   └── layout.tsx         # لایه‌بندی ریشه (HTML, فونت‌ها، پرووایدرها)
    ├── components/            # کامپوننت‌های قابل استفاده مجدد
    │   ├── ui/                # المان‌های اتمیک shadcn (Button, Input, Dialog, ...)
    │   ├── layout/            # اسکلت اصلی (Sidebar, Header, Breadcrumbs, OrgSwitcher)
    │   ├── feedback/          # سیستم اعلان‌ها، دیالوگ‌های تایید و بنرهای خطا
    │   └── data-display/      # جداول داده، چیپ‌های وضعیت، و کارت‌های KPI
    ├── features/              # کامپوننت‌ها و هوک‌های دامنه‌محور
    │   ├── auth/              # منطق ورود، خروج، و بازیابی سشن
    │   ├── resources/         # ویزارد منبع، تست اتصال، دیسکاوری دیتابیس
    │   ├── credentials/       # فرم‌های ثبت امن سکرت (Write-only)
    │   ├── plans/             # مراحل ۷‌گانه ویزارد ایجاد Backup Plan
    │   ├── runs/              # پایش بلادرنگ لاگ و نتیجه ران
    │   ├── artifacts/         # مدال تایید و اجرای وریفیکیشن و دانلود استریم
    │   └── storage/           # فرم‌های پیکربندی AWS S3, MinIO, Cloudflare R2
    ├── hooks/                 # هوک‌های عمومی کلاینت (useMediaQuery, useDebounce, ...)
    ├── lib/                   # ابزارهای زیرساختی فرانت‌اند
    │   ├── api-client.ts      # کلاینت رسمی و تایپ‌شده واکشی داده از بک‌اند Go
    │   ├── auth-context.tsx   # کانتکست نگهدارنده وضعیت کاربر و سازمان فعال
    │   ├── query-client.ts    # تنظیمات مرکزی TanStack Query
    │   ├── utils.ts           # توابع فرمت‌بندی بایت‌ها، تاریخ و رشته‌ها
    │   └── error-mapper.ts    # تبدیل کدهای خطای ماشین به پیام‌های قابل فهم کاربر
    ├── types/                 # تعاریف تایپ‌های داده منطبق با DTOهای Go
    │   ├── api.ts             # اینولوپ استاندارد پاسخ و خطا
    │   ├── auth.ts            # مدل کاربر، سشن و عضویت‌ها
    │   ├── resources.ts       # مدل منبع و کانکتورها
    │   ├── credentials.ts     # مدل کردانشال
    │   ├── plans.ts           # مدل برنامه پشتیبان‌گیری
    │   ├── runs.ts            # مدل ران و آمار
    │   ├── artifacts.ts       # مدل آرتیفکت و وریفیکیشن
    │   └── storage.ts         # مدل مقاصد ذخیره‌سازی
    └── tests/                 # زیرساخت تست‌های F1+ (Playwright, Vitest)
```

---

## ۴. معماری احراز هویت و مدیریت نشست (Authentication & Session Architecture)

معماری احراز هویت فرانت‌اند بر اساس تصمیم منجمد **ADR-006** و پیاده‌سازی بک‌اند Go به شرح زیر است:

```text
[Browser / Client]                                    [Backend Go Server]
       │                                                       │
       │─── 1. POST /api/v1/auth/login (email, password) ────►│ (Argon2id verify)
       │                                                       │
       │◄── 2. Response 200 OK ────────────────────────────────┤
       │       - JSON: { access_token (15m), user, defaultOrg }│
       │       - Set-Cookie: refresh_token (7d, HttpOnly,      │
       │                     Secure, SameSite=Strict)          │
       │                                                       │
       │ (Access token stored in-memory only)                  │
       │                                                       │
       │─── 3. Authorized Requests ───────────────────────────►│ (Verify JWT &
       │       - Header: Authorization: Bearer <access_token>  │  active session)
       │       - Header: X-Organization-ID: <active_org_id>    │
       │                                                       │
       ▼                                                       ▼
[Access Token Expires (15 min)]
       │
       │─── 4. 401 Unauthorized Intercepted ──────────────────►│
       │       (Single-flight refresh lock engaged)            │
       │                                                       │
       │─── 5. POST /api/v1/auth/refresh (via Cookie) ────────►│ (Verify & rotate
       │                                                       │  refresh token)
       │◄── 6. Response 200 OK ────────────────────────────────┤
       │       - JSON: { access_token (new 15m) }              │
       │       - Set-Cookie: new refresh_token                 │
       │                                                       │
       │─── 7. Replay queued failed requests ─────────────────►│
```

### اصول کلیدی امنیت نشست در کلاینت:
1. **نگهداری کوتاه‌مدت توکن دسترسی در حافظه رم (In-Memory Only)**:
   * مقدار `access_token` هرگز در `localStorage`، `sessionStorage` یا کوکی‌های جاوااسکریپتی ذخیره نمی‌شود.
   * با رفرش شدن تب مرورگر، کلاینت در زمان بارگذاری اولیه یک بار فراخوانی خاموش `POST /api/v1/auth/refresh` انجام داده و پس از دریافت توکن معتبر، درخواست `GET /api/v1/auth/me` را ارسال می‌کند.
2. **استفاده انحصاری از کوکی امن برای توکن رفرش**:
   * کوکی `refresh_token` دارای ویژگی‌های `HttpOnly`، `SameSite=Strict` و `Secure` (در پروداکشن) است و جاوااسکریپت به مقدار خام آن دسترسی ندارد که سیستم را در برابر حملات XSS به طور کامل ایمن می‌سازد.
3. **مکانیزم ضد طوفان رفرش (Single-Flight Token Refresh Mechanism)**:
   * هنگامی که چندین درخواست موازی هم‌زمان با خطای `401 Unauthorized` مواجه می‌شوند، یک قفل منطقی (`Promise Queue / Mutex`) فعال می‌شود تا **فقط یک درخواست** `POST /api/v1/auth/refresh` به سرور ارسال گردد.
   * کلیه درخواست‌های دیگر در صف معلق (Pending) باقی مانده و پس از تجدید موفق توکن، با هدر به‌روزشده بازپخش (Replay) می‌شوند.
   * در صورت شکست تمدید توکن (ابطال سشن در سرور یا انقضای ۷ روزه)، کلیه درخواست‌های صف با شکست قطعی مواجه شده و کاربر با پاک‌سازی استیت به صفحه لاگین هدایت می‌شود.
4. **خروج امن و ابطال فوری (Logout Flow)**:
   * با کلیک بر روی دکمه خروج، درخواست `POST /api/v1/auth/logout` با هدر احراز هویت ارسال می‌شود تا سرور سشن را در دیتابیس ابطال کرده و کوکی را با انقضای گذشته پاک کند. فرانت‌اند سپس کش سرور استیت را به طور کامل پاکسازی (`queryClient.clear()`) می‌نماید.

---

## ۵. مرز داده‌های کلاینت احرازهویت‌شده در برابر کامپوننت‌های سروری (RSC vs Authenticated Client Data Boundary)

با توجه به اینکه `access_token` صرفاً در حافظه رم مرورگر قرار دارد و کوکی `refresh_token` به عنوان یک کوکی `HttpOnly` در اختیار سرور Next.js است، مرز پردازش و واکشی داده‌ها در فاز F1 به صورت صریح منجمد می‌گردد:

1. **مرز داده‌های عملیاتی احرازهویت‌شده (Authenticated Operational State)**:
   * کلیه داده‌های حساس و عملیاتی شامل:
     * منابع سروری (`resources`)
     * برنامه‌های پشتیبان‌گیری (`plans`)
     * تاریخچه و وضعیت اجراها (`runs`)
     * فایل‌های پشتیبان (`artifacts`)
     * مقاصد ذخیره‌سازی (`storage targets`)
     * صندوقچه گواهی‌ها (`credentials`)
     * داده‌های سازمانی داشبورد (`organization dashboard metrics`)
   * **منحصراً باید در مرز کلاینت (Client Boundary)** با استفاده از ماژول متمرکز `api-client.ts` و مدیریت حالت **TanStack Query v5** با استفاده از `access_token` موجود در رم و هدر `X-Organization-ID` واکشی گردند.
2. **کاربرد مجاز کامپوننت‌های سروری (React Server Components - RSC)**:
   * کامپوننت‌های سروری صرفاً برای موارد زیر مورد استفاده قرار می‌گیرند:
     * اسکلت و شل‌های استاتیک صفحات (`Static Layouts`)
     * صفحات عمومی فاقد احراز هویت (مانند صفحه لاگین استاتیک)
     * المان‌های نمایشی که به توکن دسترسی وابسته نیستند
3. **قواعد انضباطی اکید معماری**:
   * هیچ‌گونه مخزن توکن سمت سرور در Next.js اختراع نخواهد شد.
   * توکن دسترسی (`access_token`) به هیچ عنوان در کوکی‌ها یا حافظه محلی (`localStorage`/`sessionStorage`) کپی نخواهد شد.
   * هیچ مدل احراز هویت غیرمستند BFF ایجاد نخواهد شد.
   * کلیه اصول ADR-006 شامل کوکی `HttpOnly` رفرش، توکن در رم، تمدید تک‌پروازی و کلیدهای کوئری تفکیک‌شده بر پایه سازمان حفظ خواهند شد.

---

## ۶. معماری کلاینت ارتباط با API (API Client Architecture)

تمامی ارتباطات شبکه از طریق یک ماژول متمرکز و تایپ‌شده (`lib/api-client.ts`) انجام می‌شود و فراخوانی مستقیم `fetch` در کامپوننت‌های UI ممنوع است.

### ویژگی‌های اصلی `api-client`:
* **تنظیم مسیر پایه (Base URL)**: پیش‌فرض بر روی `/api/v1` به صورت Same-Origin.
* **تزریق هدرهای اجباری به صورت خودکار**:
  * `Authorization: Bearer <access_token>` (در صورت وجود سشن لاگین).
  * `X-Organization-ID: <active_org_id>` (برای کلیه مسیرهای Tenant-Scoped).
  * `Content-Type: application/json` (برای متدهای دارای بدنه).
* **پشتیبانی از لغو درخواست‌ها (Request Cancellation)**: پذیرش خودکار `AbortSignal` از هوک‌های TanStack Query جهت جلوگیری از مصرف منابع و خطاهای Race Condition در هنگام تغییر سریع صفحات یا تب‌ها.
* **استخراج اینولوپ استاندارد خطا**: کلاینت به صورت خودکار فرمت خطای استاندارد بک‌اند را پارس کرده و یک خطای یکپارچه تایپ‌شده تولید می‌کند:
  ```typescript
  export interface ApiErrorPayload {
    code: string;       // مانند "RESOURCE_NOT_FOUND" یا "CONFLICT"
    message: string;    // پیام عمومی ترجمه‌شده سرور
    details?: unknown;  // آرایه خطاهای فیلدها در 422
    requestId?: string; // شناسه ردیابی لاگ سرور
    status: number;     // کد وضعیت HTTP
  }
  ```
* **مدیریت دانلود باینری (Stream Download Handling)**: متد اختصاصی برای دانلود آرتیفکت‌ها از طریق استریم برنامه با استفاده از `blob()` و استخراج نام فایل از هدر `Content-Disposition`.
* **ثبت ردیابی درخواست‌ها (Request ID Logging)**: ثبت `request_id` دریافتی از سرور در لاگ‌های کنسول زمان توسعه جهت تسهیل فرآیند دیباگ.

---

## ۷. مدیریت وضعیت سرور و کش‌گذاری داده‌ها (Server State & Caching)

مدیریت تمامی داده‌های واکشی‌شده از شبکه بر عهده **TanStack Query v5** است.

### ساختار کلیدهای کوئری با ایزولاسیون کامل سازمانی:
برای جلوگیری از هرگونه تداخل یا نشت اطلاعات در زمان سوییچ میان سازمان‌ها، تمامی کلیدها با ساختار سلسله‌مراتبی سازمان‌محور تعریف می‌شوند:

```typescript
export const queryKeys = {
  auth: {
    me: () => ['auth', 'me'] as const,
    organizations: () => ['auth', 'organizations'] as const,
  },
  org: (orgId: string) => ({
    all: ['org', orgId] as const,
    resources: {
      all: () => ['org', orgId, 'resources'] as const,
      detail: (id: string) => ['org', orgId, 'resources', id] as const,
      databases: (id: string) => ['org', orgId, 'resources', id, 'databases'] as const,
    },
    credentials: {
      all: () => ['org', orgId, 'credentials'] as const,
      detail: (id: string) => ['org', orgId, 'credentials', id] as const,
    },
    plans: {
      all: (filter?: Record<string, unknown>) => ['org', orgId, 'plans', filter] as const,
      detail: (id: string) => ['org', orgId, 'plans', id] as const,
    },
    runs: {
      all: (filter?: Record<string, unknown>) => ['org', orgId, 'runs', filter] as const,
      detail: (id: string) => ['org', orgId, 'runs', id] as const,
    },
    artifacts: {
      all: () => ['org', orgId, 'artifacts'] as const,
      detail: (id: string) => ['org', orgId, 'artifacts', id] as const,
    },
    storageTargets: {
      all: () => ['org', orgId, 'storage-targets'] as const,
      detail: (id: string) => ['org', orgId, 'storage-targets', id] as const,
    },
  }),
};
```

### سیاست‌های زمان ماندگاری و تازه‌سازی کش (Stale Time & Refetch Policy):
* **داده‌های ساکن و پیکربندی‌ها (Resources, Plans, Credentials, Storage)**:
  * `staleTime`: ۲ دقیقه.
  * `gcTime` (Garbage Collection): ۱۰ دقیقه.
* **داده‌های پویا و تاریخچه (Runs, Artifacts)**:
  * `staleTime`: ۱۵ ثانیه.
* **جاب‌های در حال اجرا (Active Running Jobs)**:
  * در صورتی که در صفحه ران‌ها یا داشبورد یک ران با وضعیت `running` یا `pending` وجود داشته باشد، هوک کوئری به صورت خودکار دارای `refetchInterval: 3000` (۳ ثانیه) خواهد بود تا به محض خاتمه عملیات، وضعیت به صورت زنده بدون نیاز به وب‌سوکت به‌روزرسانی شود.
* **سیاست ابطال پس از جهش (Mutation Invalidation)**:
  * ایجاد یا آرشیو Plan ➔ ابطال `plans.all()` و `resources.detail()`.
  * ایجاد Job دستی ➔ ابطال فوری `runs.all()`.
  * تایید وریفیکیشن ران ➔ ابطال `runs.detail(id)` و `artifacts.all()`.
  * حذف فیزیکی آرتیفکت ➔ ابطال `artifacts.all()` و `runs.detail()`.

---

## ۸. راهبرد استقرار یکپارچه تحت مبدأ یکسان (Same-Origin Deployment)

مطابق تصمیم **ADR-029**، معماری فرانت‌اند به گونه‌ای طراحی شده است که در هر دو محیط توسعه و پروداکشن نیاز به پیکربندی‌های شکننده CORS مرتفع گردد:

```text
                           [Reverse Proxy: Caddy or Nginx]
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼                                               ▼
         Path: /api/v1/*                                      Path: /*
         [Go Backend Server]                             [Next.js Frontend]
         (Port :8080)                                    (Port :3000)
```

### راهبرد محیط توسعه محلی (Local Development Proxy):
در زمان کدنویسی لوکال، سرور Next.js از قابلیت توکار `rewrites` در `next.config.js` استفاده می‌کند:

```javascript
/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: process.env.BACKEND_INTERNAL_URL || 'http://localhost:8080/api/v1/:path*',
      },
    ];
  },
};
module.exports = nextConfig;
```

---

## ۹. الزامات و ضوابط امنیتی فرانت‌اند (Frontend Security Rules)

1. **ممنوعیت مطلق ذخیره سکرت‌ها در LocalStorage**: هیچ کلید خصوصی، کلمه عبور، یا توکن احراز هویت بلندمدت اجازه ذخیره‌سازی در `localStorage` یا `sessionStorage` را ندارد.
2. **فرم‌های سکرت یک‌طرفه (Write-Only Secret Inputs)**:
   * فرم‌های ایجاد و ویرایش کردانشال هرگز مقدار پسورد یا کلید خصوصی قبلی را از سرور دریافت نکرده و نمایش نمی‌دهند.
   * کاربر صرفاً می‌تواند نام کردانشال را ویرایش کند یا در صورت نیاز با وارد کردن سکرت جدید، کلید را جایگزین نماید (`Replace Secret`).
3. **پنهان‌سازی ساختار فایل‌سیستم سرور (Path Non-Disclosure)**:
   * فرانت‌اند هرگز مسیرهای دیسک محلی نظیر `/srv/backup-platform/artifacts/` یا پیشوندهای باکت S3 را به کاربران نمایش نمی‌دهد.
   * نام‌گذاری فایل‌ها صرفاً بر اساس نام‌های منطقی و امن تولیدشده توسط بک‌اند بر پایه فیلد رسمی `artifact_name` نمایش داده می‌شود.
4. **عدم افشای جزئیات فنی رمزنگاری BPAE و کلیدهای مستر**:
   * آرتیفکت‌های رمزشده با استاندارد A.2 (BPAE) برای کاربر نهایی کاملاً شفاف بوده و به صورت فایل‌های دانلودی استاندارد `.sql.gz` یا `.tar.gz` ظاهر می‌شوند.
   * فرانت‌اند هیچ‌گونه جزئیاتی از نانس‌ها، تگ‌های احراز اصالت GCM، کلیدهای بسته‌بندی‌شده DEK یا نسخه‌های کلید مستر به کاربر نشان نمی‌دهد.
5. **جلوگیری از حملات تزریق کد و خطاها (Sanitized Error Rendering)**:
   * کلیه پیام‌های خطای دریافتی از سرور قبل از رندر شدن به عنوان متن خالص (Plain Text) escape می‌شوند و هیچ‌گونه خطای خام سرور مستقیماً در قالب HTML تزریق نخواهد شد.
6. **کنترل دسترسی در کلاینت صرفاً جنبه UX دارد**:
   * پنهان‌سازی دکمه‌ها یا غیرفعال‌کردن فرم‌ها بر اساس نقش کاربر (`admin`, `member`, `viewer`) صرفاً برای راهنمایی تجربه کاربری است.
   * مرجع قطعی و تفکیک‌ناپذیر اعمال امنیت و مجوزدهی، میدل‌ویرها و لایه سرویس بک‌اند Go هستند.

---

## ۱۰. استراتژی آزمون و تضمین کیفیت فرانت‌اند برای فاز F1 به بعد (Testing Strategy)

برای تضمین پایداری و سلامت فرانت‌اند در فازهای پیاده‌سازی، ابزارهای زیر منجمد می‌گردند:

### ۱. آزمون‌های واحد و کامپوننت (Unit & Component Tests):
* **ابزارها**: **Vitest** به عنوان رانر سریع و مدرن سازگار با Vite/Next.js به همراه **React Testing Library** و **@testing-library/jest-dom**.
* **محدوده**:
  * تست فرم‌ها و اعتبارسنجی اسکیمای Zod (بررسی صحت خطاها در فیلدهای نامعتبر).
  * تست نگاشت کدهای خطای سرور به پیام‌های فارسی/انگلیسی کاربرپسند.
  * تست رندرینگ مشروط المان‌ها بر پایه نقش کاربر (`RoleGuard`).
  * تست توابع کمکی تبدیل سایز بایت‌ها و فرمت تاریخ.

### ۲. آزمون‌های انتها به انتها (End-to-End Tests):
* **ابزار**: **Playwright** بر بستر براوزرهای Chromium, Firefox و WebKit.
* **سناریوهای حیاتی (Critical User Journeys)**:
  1. فرآیند لاگین موفق با ادمین سیستم و سوییچ به سازمان پیش‌فرض.
  2. ثبت یک Credential جدید (کلید SSH) و اعتبارسنجی فیلد یک‌طرفه.
  3. ثبت منبع جدید سرور ابونتو، اجرای تست اتصال و مشاهده نتیجه موفقیت‌آمیز.
  4. اجرای ویزارد چندمرحله‌ای ایجاد Backup Plan با انتخاب دیتابیس شناسایی‌شده.
  5. اجرای یک بکاپ دستی، پایش زنده تغییر وضعیت جاب تا `completed` و ران تا `success`.
  6. اعتبارسنجی آنلاین صحت فایل بکاپ (`Verify Action`) و بررسی به‌روزرسانی وضعیت به `verified`.
  7. دانلود امن آرتیفکت بکاپ و تایید نام و سایز فایل استریم‌شده.
  8. بررسی عدم دسترسی کاربر با نقش `viewer` به دکمه‌های ویرایش، اجرا، حذف یا دانلود.
  9. انقضای سشن و تمدید خودکار بی‌صدا توکن (Silent Refresh) و سناریوی خروج کامل.

### ۳. آزمون‌های دسترسی‌پذیری (Accessibility Audits):
* ادغام پکیج **`axe-core`** درون تست‌های کامپوننت و تست‌های Playwright جهت بررسی انطباق کامل المان‌ها با استاندارد WCAG 2.2 AA.
