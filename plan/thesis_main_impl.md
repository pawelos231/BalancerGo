- Automatyczna predykcja kondycji backendów z wykorzystaniem uczenia maszynowego (np. przewidywanie awarii lub \
  optymalnej wagi minimalizującej p95 na podstawie danych historycznych). \
  \- Ochronę przed przeciążeniami oraz mechanizmy stabilizujące ruch \
  \- Health checking backendów \
  \- Dynamiczny scoring backendów oparty na metrykach czasu rzeczywistego \
  \- Symulacja ataków sieciowych, m.in. SYN-flooding \
  \- Symulacja przeciążeń i nagłych skoków ruchu. \
  \- Opcjonalne mechanizmy obronne („włącz/wyłącz”), np. SYN cookies, rate limiting, ograniczanie handshake. \
  \- Adaptive routing - dynamiczne dostosowanie algorytmów kierowania ruchu, \
  \- Eksport metryk oraz narzędzia do porównywania różnych konfiguracji load balancingu \
  \- Zestaw predefiniowanych konfiguracji testowych, m.in. 1/2/4 backendy, 1000 klientów, 1000 równoległych połączeń itd. \
  \- Mechanizmy self-healing, takie jak dynamiczne modyfikowanie wag backendów lub ich automatyczne wyłączanie w \
  przypadku degradacji. \
  \- Interfejs użytkownika do wizualizacji pracy systemu w czasie rzeczywistym, obejmujący \
  &#x20;\- wykresy metryk takich jak p95/p99, throughput, obciążenie backendów, \
  &#x20;\- eksport metryk oraz możliwość zapisu danych co X milisekund do pliku (np. XLSX). \
  \- Własna, minimalna implementacja TCP oraz UDP umożliwiająca pominięcie systemowego stosu sieciowego w celu \
  uzyskania pełnej kontroli nad przepływem ruchu. \
  \- Badanie efektywności mechanizmów ‘self healingu’ \
  \- Analiza stabilności systemu dla różnych algorytmów load balancingu \
  \- Porównanie skuteczności mechanizmów obronnych przy różnych typach ataków \
  \- Optymalizacja przepustowości i wykorzystania zasobów, wraz z analizą wydajności. \
  \- Testy jednostkowe po stronie backendu i frontendu. \
  \- (Opcjonalnie) testy end-to-end, o ile pozwoli na to czas - ze względu na ich czasochłonność. "
