# Руководство по подключению VPN (iOS, Android, Windows, macOS)

Документ описывает подключение по персональной ссылке `vless://` для сервиса `ovpn`.

## Оглавление

- [1. Безопасность: используйте только официальные клиенты](#section-1)
- [2. Что нужно от администратора](#section-2)
- [3. iPhone (iOS): Streisand](#section-3)
- [4. Если приложения нет в App Store: смена региона Apple ID на US](#section-4)
- [5. Android: v2rayNG или Hiddify](#section-5)
- [6. Windows: v2rayN или Hiddify](#section-6)
- [7. macOS: Hiddify](#section-7)
- [8. Частые вопросы](#section-8)
- [9. Практические заметки из сообществ](#section-9)
- [10. Официальные источники](#section-10)

<a id="section-1"></a>
## 1. Безопасность: используйте только официальные клиенты

В магазинах и на сайтах часто встречаются подделки с похожими названиями.
Устанавливайте приложения только по официальным ссылкам:

- iOS: Streisand (App Store)
  `https://apps.apple.com/us/app/streisand/id6450534064`
- Android: v2rayNG (GitHub Releases)
  `https://github.com/2dust/v2rayNG/releases`
- Android / iOS / Windows / macOS: Hiddify (официальный GitHub)
  `https://github.com/hiddify/hiddify-app`
- iOS / macOS: Hiddify (App Store)
  `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
- Android / iOS / Windows / macOS: Hiddify (Google Play)
  `https://play.google.com/store/apps/details?id=app.hiddify.com`
- Android / iOS / Windows / macOS: Hiddify (GitHub Releases)
  `https://github.com/hiddify/hiddify-app/releases`
- Windows / Linux / macOS: v2rayN (официальный GitHub)
  `https://github.com/2dust/v2rayN`
- Windows / Linux / macOS: v2rayN (GitHub Releases)
  `https://github.com/2dust/v2rayN/releases`

Перед установкой всегда проверяйте:

1. Ссылка открывает App Store / Google Play / GitHub-репозиторий проекта.
2. Название приложения и разработчика совпадает с официальным источником.
3. Вы не используете APK/EXE со сторонних сайтов-агрегаторов.

<a id="section-2"></a>
## 2. Что нужно от администратора

Запросите у администратора:

- персональную ссылку `<ваша ссылка VPN>` (формат `vless://...`)
- при необходимости QR-код этой же ссылки

Важно:

- не публикуйте ссылку в мессенджерах и соцсетях
- не пересылайте ссылку посторонним людям

<a id="section-3"></a>
## 3. iPhone (iOS): Streisand

### Шаг 1. Установка

1. Откройте App Store.
2. Откройте официальную ссылку Streisand:
   `https://apps.apple.com/us/app/streisand/id6450534064`
3. Нажмите `Get` / `Загрузить`.

### Шаг 2. Импорт ссылки

1. Скопируйте `<ваша ссылка VPN>` в буфер обмена.
2. Откройте Streisand.
3. Нажмите `+` (добавить профиль).
4. Выберите импорт из буфера/ссылки (или импорт по QR, если у вас QR-код).
5. Сохраните профиль.

### Шаг 3. Подключение

1. Откройте созданный профиль.
2. Нажмите `Connect`.
3. Подтвердите системный запрос iOS на добавление VPN-конфигурации.

### Шаг 4. Проверка

1. Откройте браузер.
2. Перейдите на `https://ifconfig.me` или `https://ipinfo.io`.
3. Убедитесь, что IP-адрес изменился.

### Шаг 5. Если не подключается

1. Полностью закройте и откройте Streisand.
2. Удалите профиль и импортируйте ссылку повторно.
3. Проверьте, что дата/время на iPhone выставлены автоматически.
4. Отключите и снова включите Wi-Fi/мобильную сеть.
5. Запросите у администратора новую ссылку.

<a id="section-4"></a>
## 4. Если приложения нет в App Store: смена региона Apple ID на US

Ниже шаги по официальной инструкции Apple.

### Что проверить до смены региона

1. Баланс Apple Account должен быть `0`.
2. Активные подписки должны быть отменены и завершены.
3. Не должно быть незавершённых покупок/предзаказов/возвратов.
4. Если вы в Family Sharing, может потребоваться выйти из семейной группы.

### Путь в iPhone

1. `Settings`.
2. Нажмите на своё имя (Apple ID).
3. `Media & Purchases`.
4. `View Account`.
5. `Country/Region`.
6. `Change Country or Region`.
7. Выберите `United States`.
8. Примите Terms & Conditions.
9. Заполните Payment Method + Billing Address.

### Как заполнять адрес и телефон

Формат полей (пример структуры):

- Street: `<номер дома и улица>`
- City: `<город>`
- State: `<2-буквенный код штата, например CA>`
- ZIP: `<5 цифр, например 10001>`
- Phone: `+1` + `<10 цифр>`

### Примеры dummy-данных (если выбран способ оплаты `None`)

Если вы меняете регион только для установки бесплатных приложений и в форме доступен способ оплаты `None` (`No Payment Method`), обычно достаточно корректного формата контактных полей.

Примеры:

1. New York, NY
   - Street: `123 Main St`
   - City: `New York`
   - State: `NY`
   - ZIP: `10001`
   - Phone: `+1 212 555 0137`
2. Los Angeles, CA
   - Street: `742 Evergreen Terrace`
   - City: `Los Angeles`
   - State: `CA`
   - ZIP: `90001`
   - Phone: `+1 213 555 0142`

Важно:

- эти примеры подходят как шаблоны формата

<a id="section-5"></a>
## 5. Android: v2rayNG или Hiddify

### Вариант A: v2rayNG

#### Установка

1. Откройте официальный репозиторий:
   `https://github.com/2dust/v2rayNG`
2. Для прямой загрузки из GitHub используйте:
   `https://github.com/2dust/v2rayNG/releases`
3. Установите актуальную официальную версию.

#### Импорт и подключение

1. Скопируйте `<ваша ссылка VPN>`.
2. Откройте v2rayNG.
3. Нажмите `+`.
4. Выберите импорт из буфера/ссылки (или QR).
5. Выберите профиль и нажмите подключение.
6. Подтвердите системный VPN-запрос Android.

### Вариант B: Hiddify

#### Установка

1. Откройте Google Play или официальный GitHub:
   `https://github.com/hiddify/hiddify-app`
2. Google Play (прямая ссылка):
   `https://play.google.com/store/apps/details?id=app.hiddify.com`
3. GitHub Releases (прямая ссылка):
   `https://github.com/hiddify/hiddify-app/releases`
4. Установите официальную версию.

#### Импорт и подключение

1. Скопируйте `<ваша ссылка VPN>`.
2. Откройте Hiddify.
3. Выберите импорт ссылки/буфера (или QR).
4. Сохраните профиль.
5. Нажмите `Connect`.
6. Подтвердите системный VPN-запрос.

### Проверка

1. Откройте `https://ifconfig.me`.
2. Проверьте, что IP-адрес изменился.

### Если приложение недоступно в Google Play

Google указывает, что доступ зависит от страны профиля Google Play.
Смена страны в Google Play требует:

1. Фактического нахождения в новой стране.
2. Способа оплаты из новой страны.
3. Ожидания между сменами страны (обычно не чаще, чем раз в 90 дней).

<a id="section-6"></a>
## 6. Windows: v2rayN или Hiddify

### Вариант A: v2rayN

#### Шаг 1. Установка

1. Откройте официальный репозиторий:
   `https://github.com/2dust/v2rayN`
2. Прямая ссылка на релизы:
   `https://github.com/2dust/v2rayN/releases`
3. Скачайте архив с ядром (обычно `v2rayN-With-Core...zip`).
4. Распакуйте в отдельную папку.
5. Запустите `v2rayN.exe`.

#### Шаг 2. Импорт ссылки

1. Скопируйте `<ваша ссылка VPN>`.
2. В v2rayN выполните импорт из буфера/ссылки.
3. Проверьте, что профиль появился в списке.

#### Шаг 3. Подключение

1. Выберите профиль активным.
2. Включите подключение.
3. При необходимости включите `System Proxy` или `TUN`.

#### Шаг 4. Проверка

1. Откройте `https://ifconfig.me`.
2. Убедитесь, что IP изменился.

#### Шаг 5. Если не работает

1. Запустите v2rayN от имени администратора.
2. Убедитесь, что используется core `Xray`.
3. Если не открываются отдельные сайты (например, YouTube/Instagram), сначала проверьте режим `TUN`.
4. Переключите режим `System Proxy`/`TUN`.
5. Повторно импортируйте ссылку.

### Вариант B: Hiddify (Windows)

#### Шаг 1. Установка

1. Откройте официальный репозиторий:
   `https://github.com/hiddify/hiddify-app`
2. Прямая ссылка на релизы:
   `https://github.com/hiddify/hiddify-app/releases`
3. Скачайте и установите Windows-версию.

#### Шаг 2. Импорт и подключение

1. Скопируйте `<ваша ссылка VPN>`.
2. Откройте Hiddify.
3. Выберите импорт ссылки/буфера (или QR).
4. Сохраните профиль.
5. Нажмите `Connect`.

#### Шаг 3. Проверка

1. Откройте `https://ipinfo.io` или `https://ifconfig.me`.
2. Проверьте, что IP-адрес изменился.

<a id="section-7"></a>
## 7. macOS: Hiddify

### Шаг 1. Установка

1. Откройте официальный репозиторий:
   `https://github.com/hiddify/hiddify-app`
2. Прямая ссылка на релизы:
   `https://github.com/hiddify/hiddify-app/releases`
3. Для версии из App Store (если нужна) используйте:
   `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
4. Установите приложение.

### Шаг 2. Импорт ссылки

1. Скопируйте `<ваша ссылка VPN>`.
2. Откройте Hiddify.
3. Выберите импорт из буфера/ссылки.
4. Сохраните профиль.

### Шаг 3. Подключение

1. Выберите профиль.
2. Нажмите `Connect`.
3. Подтвердите системные разрешения macOS (VPN/network extension).

### Шаг 4. Проверка

1. Откройте `https://ipinfo.io`.
2. Проверьте, что IP изменился.

<a id="section-8"></a>
## 8. Частые вопросы

### Почему в ссылке есть `encryption=none`?

Для `VLESS` это нормально. Защита обеспечивается транспортом `REALITY/TLS`.

### Нужно ли менять SNI вручную?

Обычно нет. Если администратор выдал готовую ссылку, параметры уже настроены.

### Почему иногда подключается, но сайты не открываются?

Обычно причина в режиме маршрутизации или системном прокси в клиенте.
Для `v2rayN` на Windows в первую очередь проверьте `TUN` (в реальном кейсе это сразу восстановило доступ к YouTube/Instagram), затем переключите режим (`System Proxy`/`TUN`) и повторите проверку IP.
Подробный чеклист: `docs/troubleshooting-v2rayn-youtube-instagram-ru.md`.

<a id="section-9"></a>
## 9. Практические заметки из сообществ

По обсуждениям в Apple Community и Reddit чаще всего встречаются такие причины проблем:

1. Не удаётся сменить регион App Store из-за активных подписок или остатка баланса.
2. Ошибка `payment method is not valid in this store` при несовпадении страны карты/адреса.
3. Смена страны в Google Play недоступна сразу и может потребовать ожидания.

Это не официальные правила, но типовые кейсы пользователей совпадают с ограничениями из официальных справок Apple/Google.

<a id="section-10"></a>
## 10. Официальные источники

- Apple: Change your Apple Account country or region
  `https://support.apple.com/en-us/118283`
- Apple: Payment methods that you can use with your Apple Account
  `https://support.apple.com/en-us/111741`
- Google Play Help: How to change your Google Play country
  `https://support.google.com/googleplay/answer/7431675`
- Project X (официальный клиентский раздел)
  `https://xtls.github.io/ru/document/level-0/ch08-xray-clients.html`

Официальные страницы клиентов:

- Streisand (iOS)
  `https://apps.apple.com/us/app/streisand/id6450534064`
- v2rayNG
  `https://github.com/2dust/v2rayNG`
- v2rayNG (Releases)
  `https://github.com/2dust/v2rayNG/releases`
- v2rayN
  `https://github.com/2dust/v2rayN`
- v2rayN (Releases)
  `https://github.com/2dust/v2rayN/releases`
- Hiddify
  `https://github.com/hiddify/hiddify-app`
- Hiddify (App Store)
  `https://apps.apple.com/us/app/hiddify-proxy-vpn/id6596777532`
- Hiddify (Google Play)
  `https://play.google.com/store/apps/details?id=app.hiddify.com`
- Hiddify (Releases)
  `https://github.com/hiddify/hiddify-app/releases`

Полезные обсуждения (сообщества):

- Apple Community: проблемы смены региона
  `https://discussions.apple.com/thread/256239483`
- Apple Community: частые причины блокировки смены региона
  `https://discussions.apple.com/thread/255055483`
- Reddit (Google Play): сложности смены страны
  `https://www.reddit.com/r/googleplay/comments/terz1a/changing_country_on_google_play_store_account/`
