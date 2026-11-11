# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Studenci cywilni Wojskowej Akademii Technicznej (WAT) w Warszawie. Sprawdzają plan głównie na telefonie, w biegu: przed wyjściem z domu, między zajęciami, w drodze do sali. Ich zadanie: szybko wiedzieć, co mają teraz i dalej, gdzie (sala, budynek) i z kim, oraz ile zajęć z danego przedmiotu jeszcze zostało.

Interfejs jest po polsku. Grupy identyfikowane są kodami WAT (np. `WEL26EY4S0`, `WMT26AX3S1`), które studenci znają na pamięć.

## Product Purpose

WATplan zamienia oficjalne strony planów Plansoft publikowane przez wydziały WAT w czytelny, mobilny plan zajęć. Sukces: student otwiera stronę i w kilka sekund wie, dokąd iść, bez powiększania tabeli Plansoft.

Darmowy, open source (MIT), niezależny projekt studencki, niepowiązany z WAT. Oficjalny plan WAT jest zawsze źródłem prawdy.

## Positioning

Ten sam publiczny plan co Plansoft, ale:
- czytelny na telefonie (widok dnia zamiast tabeli całego semestru),
- z licznikami „Ile jeszcze”: przy każdych zajęciach `x/y`, lista wszystkich wystąpień przedmiotu danej formy, ile zostało i kiedy jest ostatnie,
- z wykrywaniem zmian w planie (porównanie z poprzednim pobraniem, data modyfikacji),
- z eksportem do kalendarza (.ics dla Google, Apple, Outlook).

## Operating Context

- Użycie: telefon, często jedną ręką, w ruchu, w świetle dziennym i wieczorem (tryb jasny i ciemny, domyślnie wg systemu).
- Wybór grupy raz, zapamiętywany na urządzeniu; link z `?plan=` do udostępnienia grupie.
- Widoki: Dzień (domyślny), Tydzień, Miesiąc.
- Zajęcia odbywają się w stałych blokach 08:00–21:15 (7 bloków po 95 min); kolory przedmiotów pochodzą z planów WAT.

## Capabilities and Constraints

- Backend w Go pobiera strony Plansoft z serwerów wydziałowych (WEL, WML, WLO, WIM, IOE, WTC), parsuje je do JSON i serwuje frontend (statyczne HTML/CSS/JS bez frameworka).
- Hosting: Render (darmowy plan, usypia serwis, efemeryczny dysk). Zapisane plany w `data/` w repo są trwałym cache.
- Serwery WAT stoją za Imperva/Incapsula. Pobieranie musi być oszczędne (on-demand, ~1 zapytanie/s, wstrzymanie domeny po blokadzie). Gdy dane są stare, aplikacja mówi to wprost (baner z datą pobrania) zamiast udawać aktualność.
- Dane bywają niepełne: brak prowadzącego (np. lektoraty, część ćwiczeń WEL), nietypowe godziny, sale w formacie WAT (`315 S`, `3.8 75`).
- Wydziały zmieniają foldery planów między semestrami; adresy źródeł trzeba aktualizować.

## Brand Commitments

- Nazwa: **WATplan**.
- Ton: rzeczowy, po polsku, po studencku, bez marketingu.
- Projekt niezależny: bez logo i symboli WAT udających oficjalną aplikację uczelni.

## Evidence on Hand

- Realne plany wszystkich grup w `data/*.json` (stan z 19.10.2026, część odświeżana później).
- Brak opinii użytkowników, statystyk ani testimoniali; nie wymyślać ich.

## Product Principles

1. Najpierw „co teraz i gdzie”: najkrótsza droga od otwarcia do sali.
2. Uczciwość danych: zawsze widać, z kiedy jest plan; oficjalny plan WAT ma pierwszeństwo.
3. Telefon jest głównym ekranem; komputer to dodatek.
4. Szacunek dla serwerów WAT: minimum zapytań, żadnego obchodzenia zabezpieczeń.

## Accessibility & Inclusion

Czytelność w słońcu i nocą (kontrast w obu motywach), cele dotyku co najmniej 44 px, kolor przedmiotu nigdy nie jest jedynym nośnikiem informacji (zawsze nazwa i forma zajęć).
