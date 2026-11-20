package wat

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

const proxyPause = 30 * time.Minute

var errProxySwitched = errors.New("przełączono na zapasowe proxy")

func (f *Fetcher) currentProxy() *url.URL {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return nil
	}
	if f.active != 0 && time.Now().After(f.proxyBad[0]) {
		f.active = 0
	}
	return f.proxies[f.active]
}

func (f *Fetcher) proxyPaused() (time.Time, bool) {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return time.Time{}, false
	}
	now := time.Now()
	var soonest time.Time
	for _, until := range f.proxyBad {
		if !now.Before(until) {
			return time.Time{}, false
		}
		if soonest.IsZero() || until.Before(soonest) {
			soonest = until
		}
	}
	return soonest, true
}

func (f *Fetcher) proxyFailed(used *url.URL, status string) error {
	f.proxyMu.Lock()
	now := time.Now()
	f.proxyError, f.proxyAt = used.Host+" "+status, now
	next := -1
	for i, p := range f.proxies {
		if p == used {
			f.proxyBad[i] = now.Add(proxyPause)
		}
	}
	for i := range f.proxies {
		if f.proxies[i] != used && now.After(f.proxyBad[i]) {
			next = i
			break
		}
	}
	if next >= 0 {
		f.active = next
	}
	f.proxyMu.Unlock()

	if next >= 0 {
		backup := f.proxies[next].Host
		log.Printf("proxy %s odrzuciło połączenie: %s - przełączam na %s", used.Host, status, backup)
		f.alert.Problem("proxy:"+used.Host, fmt.Sprintf("Proxy %s odrzuca połączenia: %s. Przełączyłem na zapasowe %s. Sprawdź konto u dostawcy.", used.Host, status, backup))
		return fmt.Errorf("%w: %s", errProxySwitched, backup)
	}
	log.Printf("proxy %s odrzuciło połączenie: %s - brak działającego proxy, wstrzymuję pobieranie na %v", used.Host, status, proxyPause)
	f.alert.Problem("proxy", fmt.Sprintf("Żadne proxy nie działa (ostatnie: %s, %s). Pobieranie z WAT stoi. Sprawdź konta u dostawców proxy.", used.Host, status))
	return fmt.Errorf("%w: %s %s - sprawdź konto u dostawcy proxy", ErrProxy, used.Host, status)
}

func (f *Fetcher) proxyWorked(used *url.URL) {
	f.proxyMu.Lock()
	f.proxyError = ""
	for i, p := range f.proxies {
		if p == used {
			f.proxyBad[i] = time.Time{}
		}
	}
	f.proxyMu.Unlock()
	f.alert.Resolved("proxy", "Pobieranie przez proxy znów działa ("+used.Host+").")
	f.alert.Resolved("proxy:"+used.Host, "Proxy "+used.Host+" znów działa.")
}

func (f *Fetcher) ProxyHost() string {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return ""
	}
	hosts := make([]string, len(f.proxies))
	for i, p := range f.proxies {
		hosts[i] = p.Host
		if i == f.active {
			hosts[i] += " (aktywne)"
		}
	}
	return strings.Join(hosts, ", ")
}
