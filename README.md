# 📅 WatPlan 

**WatPlan** to lekka, błyskawiczna i nowoczesna aplikacja webowa napisana w **Go**, służąca do parsowania i wyświetlania planów zajęć studentów Wojskowej Akademii Technicznej (WAT). 

Zamiast przedzierać się przez nieczytelne, przestarzałe tabele HTML na stronach uczelni, WatPlan pobiera dane, przetwarza je w locie i serwuje w postaci przejrzystego, responsywnego kalendarza – idealnego do szybkiego sprawdzania zajęć na telefonie w drodze na uczelnię.

## ✨ Główne funkcje

* 📱 **Responsywny interfejs (Mobile-First):** Kalendarz płynnie dopasowuje się do ekranów smartfonów, pozwalając na wygodne przewijanie i czytanie bez powiększania ekranu.
* 🌙 **Ciemny motyw (Dark Mode):** Wbudowany przełącznik motywu jasny/ciemny. Domyślnie ustawiony na tryb ciemny, aby oszczędzać wzrok (i baterię) podczas wieczornych powtórek. Preferencja zapisuje się w pamięci przeglądarki (`localStorage`).
* 🧠 **Inteligentna nawigacja:** Aplikacja automatycznie grupuje zajęcia i po wejściu na stronę domyślnie wyświetla **aktualny tydzień semestru**. Przeskakiwanie między tygodniami odbywa się za pomocą wygodnych strzałek.
* 🔗 **Udostępnianie linków (Direct Routing):** Możesz wysłać znajomemu z innego wydziału bezpośredni link do jego planu (np. `?plan=WEL_KOLEGA`). Urządzenie automatycznie zapamięta ten wybór na przyszłość.
* 🛡️ **Omijanie blokad WAF (Incapsula/Imperva):** Uczelniane serwery potrafią blokować ruch z chmury (np. Render, AWS). WatPlan posiada wbudowaną, bezpieczną obsługę zewnętrznych Proxy przez zmienne środowiskowe, aby zapewnić ciągłość aktualizacji z serwerów WAT.
* ⚡ **Bez zależności (Zero-dependency Frontend):** Frontend oparty jest na czystym HTML, CSS i Vanilla JS. Backend używa `go:embed` do pakowania wszystkich zasobów w jeden wydajny plik binarny.

## 🛠️ Technologie

* **Backend:** Go (Golang) + `goquery` (do parsowania struktury HTML).
* **Frontend:** Vanilla JavaScript, natywny CSS (z użyciem zmiennych CSS).
* **Baza danych:** Brak. Aplikacja przechowuje zbuforowane i sparsowane plany w prostych plikach `.json` w katalogu `data/`, co czyni ją banalnie prostą w hostowaniu i utrzymaniu.

## 🚀 Szybki start (Uruchomienie lokalne)

1. Sklonuj repozytorium:
   ```bash
   git clone [https://github.com/billypuding/watplan.git](https://github.com/billypuding/watplan.git)
   cd watplan