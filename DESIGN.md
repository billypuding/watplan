---
name: WATplan
description: Szybki i darmowy plan zajęć dla wydziałów WAT. Bez logowania, bez reklam.
colors:
  paper: "#f2f4f7"
  surface: "#ffffff"
  surface-sunken: "#e7ebf0"
  rule-line: "#cfd6df"
  ink: "#0b1220"
  ink-muted: "#475467"
  signal-teal: "#0f766e"
  signal-teal-ink: "#ffffff"
  alarm-red: "#b42318"
  stale-amber-bg: "#fff4e0"
  stale-amber-ink: "#6b3d00"
  night: "#0d1117"
  night-surface: "#161b22"
  night-surface-sunken: "#1d232c"
  night-rule-line: "#262d37"
  night-ink: "#e6edf3"
  night-ink-muted: "#8b96a5"
  night-signal-teal: "#2dd4bf"
  night-signal-teal-ink: "#04221f"
  night-alarm-red: "#ff8a7a"
  night-stale-amber-bg: "#3a2a10"
  night-stale-amber-ink: "#ffd9a0"
  star-amber: "#b7791f"
  night-star-amber: "#f5a524"
typography:
  display:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "32px"
    fontWeight: 800
    lineHeight: 1.1
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 800
    lineHeight: 1.15
  hero:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "72px"
    fontWeight: 800
    lineHeight: 0.98
    letterSpacing: "-0.045em"
  board:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "17px"
    fontWeight: 800
    lineHeight: 1.1
    letterSpacing: "-0.01em"
    fontFeature: "tnum"
  title:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 800
    lineHeight: 1.25
  body:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 500
    lineHeight: 1.4
  label:
    fontFamily: "Bricolage Grotesque, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 700
    lineHeight: 1.3
rounded:
  cell: "10px"
  field: "14px"
  card: "18px"
  pill: "22px"
  sheet: "24px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
components:
  button-round:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    size: "44px"
  button-round-hover:
    backgroundColor: "{colors.surface-sunken}"
  button-today:
    backgroundColor: "{colors.signal-teal}"
    textColor: "{colors.signal-teal-ink}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 16px"
  chip-group:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 14px"
  day-pill-selected:
    backgroundColor: "{colors.signal-teal}"
    textColor: "{colors.signal-teal-ink}"
    rounded: "{rounded.card}"
    height: "60px"
  segmented:
    backgroundColor: "{colors.surface-sunken}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.field}"
    padding: "4px"
  segmented-selected:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.cell}"
    height: "40px"
  search-field:
    backgroundColor: "{colors.surface-sunken}"
    textColor: "{colors.ink}"
    rounded: "{rounded.field}"
    height: "48px"
    padding: "0 14px"
  button-primary:
    backgroundColor: "{colors.signal-teal}"
    textColor: "{colors.signal-teal-ink}"
    rounded: "{rounded.pill}"
    height: "46px"
    padding: "0 18px"
  stale-banner:
    backgroundColor: "{colors.stale-amber-bg}"
    textColor: "{colors.stale-amber-ink}"
    rounded: "{rounded.field}"
    padding: "10px 14px"
---

# Design System: WATplan

## Overview

**Creative North Star: "Tablica przy sali w kieszeni"**

WATplan czyta się jak tablica informacyjna przy sali wykładowej, tylko zmieszczona w telefonie i podana na jedno zerknięcie między zajęciami. Godzina i sala zawsze stoją na pierwszym planie, duże, pogrubione, z cyframi o stałej szerokości; nazwa przedmiotu, forma zajęć i prowadzący są drugą linią informacji. Wszystko inne (nawigacja, ustawienia, eksport) schodzi na dalszy plan i mieści się w trzech okrągłych przyciskach nagłówka.

Nastrój jest konkretny i szybki: jeden krój (Bricolage Grotesque), jedna barwa akcentu (sygnałowy turkus) zarezerwowana na „tu jesteś” i „teraz”, oraz kolory przedmiotów z planów WAT, przefiltrowane do spokojnych odcieni, żeby nie krzyczały jak w Plansoft. Ciemny motyw jest wzorcem; jasny dorównuje mu kontrastem, a nie jest jego wyblakłą kopią. Oba wybiera system, użytkownik może to zmienić.

WATplan nie jest tabelą Plansoft (gęsta siatka semestru, jaskrawe tła, drobny tekst), nie udaje oficjalnej aplikacji uczelni (bez logo WAT, bez urzędowego granatu) i nie jest generycznym „AI dashboardem” (bez gradientów, świecących cieni, kart w kartach).

**Key Characteristics:**
- Godzina po lewej, a w karcie przedmiot, forma z licznikiem oraz sala i prowadzący z ikonami: układ wiersza tablicy.
- Jeden krój, skala o małych skokach, cyfry tabelaryczne wszędzie, gdzie stoi czas, sala lub licznik.
- Akcent tylko dla stanu: wybrany dzień, „teraz”, „następne”, przycisk „Dziś”.
- Kolor przedmiotu to tło karty, nigdy jedyny nośnik informacji.
- Uczciwość danych na wierzchu: baner z datą pobrania, gdy plan jest stary.

## Colors

Chłodne neutralne tła i tusz, jeden sygnałowy turkus, a cała reszta koloru pochodzi z przedmiotów.

### Primary
- **Sygnałowy turkus** (signal-teal / night-signal-teal): wybrany dzień w pasku tygodnia, przycisk „Dziś”, obwódka trwających zajęć i „następnych” w serii, fokus klawiatury, zaznaczenie tekstu, przycisk eksportu. Nigdy dekoracja.

### Neutral
- **Papier / Noc** (paper / night): tło strony.
- **Powierzchnia** (surface / night-surface): przyciski nagłówka, arkusze, puste stany.
- **Wgłębienie** (surface-sunken / night-surface-sunken): pole wyszukiwania, tor przełącznika segmentowego, przyciski drugorzędne, hover.
- **Linia** (rule-line / night-rule-line): obramowania przycisków, separatory ustawień, linia przerwy.
- **Tusz** (ink / night-ink): cały tekst główny.
- **Tusz przygaszony** (ink-muted / night-ink-muted): godzina końca, podtytuły, etykiety pól, dni poza miesiącem. Na tle strony i powierzchni ≥ 4.5:1.

### Tertiary
- **Bursztyn nieaktualności** (stale-amber-bg/ink i nocne odpowiedniki): wyłącznie baner „plan może być nieaktualny”.
- **Alarmowa czerwień** (alarm-red / night-alarm-red): komunikaty błędów.

### Odcienie przedmiotów
Kolor przedmiotu z planu WAT zamieniany jest w JS na barwę (`--h`) i flagę nasycenia (`--s`, 0 dla szarości), a CSS buduje z nich zestaw per motyw w HSL:

| rola | jasny (nasycenie, jasność) | ciemny |
|---|---|---|
| tło karty | 75%, 90% | 32%, 18% |
| tekst | 60%, 14% | 70%, 88% |
| tekst drugorzędny | 40%, 27% | 30%, 74% |
| kropka / pasek postępu | 75%, 40% | 75%, 62% |
| tor paska | 45%, 80% | 25%, 28% |
| obramowanie | 45%, 80% | 32%, 18% (zlewa się z tłem) |

**The Quiet Subject Rule.** Kolor WAT nigdy nie trafia na ekran surowy (`#FFFF00`, `#FF0000`). Zawsze przechodzi przez powyższą tabelę, żeby każdy przedmiot miał ten sam kontrast tekstu.

**The Signal Only Rule.** Turkus oznacza stan albo główną akcję. Jeśli element jest turkusowy, a nic nie jest „wybrane”, „teraz” ani „do kliknięcia jako główne”, to błąd.

## Typography

**Display Font:** Bricolage Grotesque (z ui-sans-serif, system-ui)
**Body Font:** Bricolage Grotesque

**Character:** Jeden grotesk z charakterem, z optycznymi rozmiarami (opsz 12–96), w wagach 500–800: przy dużych rozmiarach ciasny i wyrazisty, w treści spokojny. Ma polskie znaki i cyfry tabelaryczne. Hierarchię niesie waga i rozmiar, nie zmiana kroju.

### Hierarchy
- **Display** (800, 32px, 1.1, -0.02em): nazwa dnia w widoku dnia.
- **Headline** (800, 22px, 1.15): tytuły arkuszy i ekranu „Ile jeszcze”.
- **Hero** (800, 46px na telefonie / 72px od 900px, 0.98, -0.045em): nagłówek strony głównej w dwóch linijkach, druga w turkusie.
- **Board** (800, 17px, cyfry tabelaryczne): godzina rozpoczęcia w lewej kolumnie; ta sama rodzina dla licznika serii (44px).
- **Title** (800, 16px, 1.25): nazwa przedmiotu.
- **Body** (500, 15px, 1.4): podtytuły, opisy, puste stany.
- **Label** (700, 12–13px): forma zajęć z licznikiem `x/y`, prowadzący, nagłówki grup w wyszukiwarce, dni tygodnia.

**The Tabular Rule.** Każda godzina, sala, data i licznik używa `font-variant-numeric: tabular-nums`, żeby kolumny się nie przesuwały.

**The No Eyebrow Rule.** Nad nazwą przedmiotu nie stoi etykieta wersalikami. Forma zajęć idzie pod nazwą, zwykłymi literami.

## Layout

Jedna kolumna o maks. 560px z marginesem 16px (z uwzględnieniem `safe-area-inset`); widok tygodnia poszerza kontener do 1100px. Wiersz zajęć to siatka `48px | 1fr`: kolumna godzin i karta. Karta to siatka `1fr | auto`: treść i sala; pasek „trwa teraz” rozciąga się na obie kolumny. Odstępy w rytmie 4/8/12/16/24px: 10px między kartami, 16px między sekcjami.

Siatka tygodnia ma kolumnę godzin (34px, 54px od 720px) i 5 lub 7 kolumn dni; pokazuje tylko bloki od pierwszego do ostatniego zajętego. Poniżej 720px komórki pokazują skrót przedmiotu, powyżej pełną nazwę. Miesiąc to siatka 7×(5–6) z kropkami przedmiotów i listą dnia pod spodem.

**The Thumb Rule.** Każdy cel dotyku ma co najmniej 44px wysokości; nawigacja jest w zasięgu kciuka albo przesunięciem palca w bok.

## Elevation & Depth

System jest płaski. Głębię daje kontrast tonów (strona → powierzchnia → wgłębienie) i 1px linie. Cień pojawia się tylko na elementach, które naprawdę leżą nad treścią: arkuszach dialogów i komunikacie (toast).

### Shadow Vocabulary
- **Arkusz** (`0 1px 2px rgba(11,18,32,.08), 0 10px 30px rgba(11,18,32,.10)`; w nocy `0 1px 2px rgba(0,0,0,.4), 0 12px 32px rgba(0,0,0,.45)`): dialogi wyboru grupy i ustawień, toast.
- **Obwódka stanu** (`0 0 0 2px <signal-teal>`): trwające zajęcia, „następne” w serii. Nie jest cieniem, tylko pierścieniem stanu.

**The Flat Card Rule.** Karty zajęć nie mają cienia. Odróżniają się tłem przedmiotu i 1px obramowaniem w jego odcieniu.

## Shapes

Łagodne, spójne zaokrąglenia: komórki siatki 10px, pola i przełączniki 14px, karty 18px, przyciski w pełni okrągłe (22px przy 44px wysokości), arkusze 24px u góry. Ikony rysowane jednym stylem: obrys 2px, zaokrąglone końce, 14–20px.

## Components

### Buttons
- **Shape:** w pełni zaokrąglone (22px), wysokość 44px.
- **Okrągły (nagłówek):** powierzchnia + 1px linia, ikona w tuszu; hover = wgłębienie. Odświeżanie obraca ikonę podczas pobierania (przy ograniczonym ruchu tylko przygasza).
- **„Dziś”:** turkus z białym tekstem, widoczny tylko wtedy, gdy dzisiejszy dzień nie jest na ekranie.
- **Główny (eksport .ics):** turkus, 46px.
- **Drugorzędny (link-btn, ghost):** wgłębienie, tusz.

### Chips
- **Grupa w nagłówku:** powierzchnia + linia, kod grupy wagą 700, strzałka w dół.
- **Formy zajęć w serii:** obrys; wybrana forma odwrócona (tusz jako tło).

### Cards / Containers
- **Karta zajęć:** tło i 1px obramowanie w odcieniu przedmiotu, 18px, padding 12×14, bez cienia. Minione zajęcia tracą kolor (`saturate(0)`) i lekko przygasają, ale tekst zostaje ≥ 4.5:1.
- **Pusty dzień:** powierzchnia z przerywaną linią, wyśrodkowany tekst i przycisk „Następne zajęcia”.

### Inputs / Fields
- **Wyszukiwarka grupy:** wgłębienie, 48px, 14px, ikona lupy w tuszu przygaszonym; kursor w turkusie.
- **Przełącznik segmentowy:** tor we wgłębieniu, wybrana opcja na powierzchni z delikatnym cieniem 1px.

### Navigation
- **Pasek tygodnia:** strzałki + 7 przycisków dnia (60px). Dni z zajęciami mają kropkę w turkusie, dzisiejszy podkreślony numer, wybrany wypełniony turkusem.
- **Wiersz nawigacji (tydzień, miesiąc):** okrągłe strzałki po bokach, zakres i liczba zajęć pośrodku.

### Karta zajęć jako wiersz tablicy (signature)
Godzina startu (17px, 800) nad godziną końca (13px, przygaszona) w lewej kolumnie. W karcie: nazwa przedmiotu, pod nią kropka koloru + forma + licznik `x/y`, a niżej linijka z ikoną budynku i salą (pogrubiona, cyfry tabelaryczne; przy kilku salach naraz „+4 sale”) oraz ikoną osoby i prowadzącym. Trwające zajęcia dostają obwódkę stanu i pasek postępu z „zostało N min”.

### Ulubione
- **Gwiazdka** przy każdej grupie w wyszukiwarce; włączona w bursztynie (star-amber / night-star-amber), wyłączona jako obrys w tuszu przygaszonym.
- **Przełącznik w aplikacji:** pasek chipów pod nagłówkiem, obecny plan wypełniony turkusem; pokazuje się, gdy w ulubionych jest inny plan niż obecny.
- **W oknie wyboru grupy:** sekcja „Ulubione” z chipami i przyciskiem ✕ do usuwania.
- **Na stronie głównej:** chipy pod polem grupy otwierają plan jednym kliknięciem.

### Strona główna
Ciemna, jeden ekran bez przewijania ze stopką zawsze widoczną na dole; dwie kolumny od 900px (na telefonie jedna pod drugą). Przykładowy plan pokazuje tyle kart, ile się zmieści: 4 na komputerze (3 przy wysokości poniżej 760px), 2 na typowym telefonie, 1 na niskim, a poniżej 600px wysokości na telefonie znika. Po lewej duże logo z nazwą (52px, od 900px 64px), nagłówek Hero w dwóch linijkach „Szybki i darmowy.” / „Plan dla wszystkich wydziałów WAT.” (druga w turkusie), podtytuł „Bez logowania, bez reklam.”, duże pole grupy (64px) z przyciskiem „Otwórz plan” i ulubionymi. Po prawej sam przykładowy plan dnia, bez nagłówka i nazwy grupy: przedmioty, sale, godziny i prowadzący z prawdziwego planu, dane wpisane na stałe. Ruch (około 0,6s, sam CSS): znak logo się składa (kartka, kafelki zajęć, linia „teraz” przejeżdża od lewej), a karty przykładowego planu spadają jak klapki, jedna po drugiej. Nazwa, hasło i pole grupy są widoczne od razu; otwarcie planu jest natychmiastowe. Przy prefers-reduced-motion i przy przekierowaniu stałego użytkownika bez animacji. Stopka: autor i kontakt, drobnym drukiem informacja, że to nieoficjalne narzędzie.

## Do's and Don'ts

### Do:
- **Do** stawiaj godzinę jako największą liczbę w wierszu (17px, waga 800, cyfry tabelaryczne), a salę pogrubioną przy ikonie budynku.
- **Do** przepuszczaj każdy kolor WAT przez tabelę odcieni przedmiotów w obu motywach.
- **Do** sprawdzaj każdą parę tekst/tło w obu motywach: ≥ 4.5:1 dla tekstu, ≥ 3:1 dla kropek i pasków.
- **Do** pokazuj datę pobrania planu, gdy jest starszy niż doba (bursztynowy baner).
- **Do** utrzymuj cele dotyku ≥ 44px i rytm odstępów 4/8/12/16/24px.

### Don't:
- **Don't** wracaj do wyglądu tabeli Plansoft: siatki całego semestru, jaskrawych teł, tekstu poniżej 12px.
- **Don't** używaj logo, herbu ani granatu WAT, ani niczego, co udaje oficjalną aplikację uczelni.
- **Don't** używaj gradientów, świecących cieni, efektu szkła ani kart w kartach.
- **Don't** przygaszaj minionych zajęć samą przezroczystością poniżej 0.8; odbieraj im kolor, nie czytelność.
- **Don't** stawiaj etykiety wersalikami nad nazwą przedmiotu ani nad nagłówkami.
