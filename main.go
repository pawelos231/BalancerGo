package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}

/**
retransmisja pakietów powinna się dziać ze strony serwera i ze strony klienta jeżeli nie zostało wysłane ACK na czas, latency histograms można zrobić per server, grupowanie streamów na równoległych klientów którzy wysyłają te pakiety ma sens, gdzie dla każdego klienta jest osobna go routyna, wydaje mi się że później serwer powinien mieć zamiast może handlePacket czy jakiś tam packetProcessor, może żeby po prostu miał handleStream of packets no i będzie on dostawał emisje tych pakietów po prostu od klientów i jeżeli powiedzmy dany stream nie zakończy tego połączenia albo coś w tym stylu, nie wyśle ACK to będziemy mieli retry logic, oczywiście żeby to się wydarzyło to musielibyśmy na początku osobno mieć stworzonego klienta którym to celem jest transmisja tych pakietów i odbieranie ich od serwera i również weryfikowanie czy zostały dostarczone wszystkie pakiety, wtedy mielibyśmy pełną symulacje serwera i klienta, i do tego do środka pomiędzy serwerów i klientów wrzucić ten load balancer który w osobnych go routynach odpala serwery i klientów i które zajmuje się routowaniem tych połączeń zważając również uwagę na obronę przed flood attacks itd itp, czy to o czym mówie ma sens, spójrz na to krytycznie

*/
