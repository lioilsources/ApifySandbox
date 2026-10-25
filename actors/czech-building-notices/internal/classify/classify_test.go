package classify

import "testing"

// Titles taken from real notice boards harvested on 2026-10-25 (names of
// private persons replaced by invented ones).
func TestClassify(t *testing.T) {
	cases := []struct {
		title, agenda string
		want          Type
	}{
		{"Veřejná vyhláška Vyrozumění o zahájení řízení o povolení záměru", "", Zahajeni},
		{"vyrozumění o zahájení Ř o povolení záměru: Krematorium - ul. Křelovská, cykloste", "", Zahajeni},
		{"SMO/731538/26/Vnitř./Trv\nVyrozumění o zahájení řízení ve věci žádosti o povolení záměru „Okružní křižovatka silnic II/477", "", Zahajeni},
		{"rozhodnutí - povolení stavby - zrychlené řízení: \"Rekonstrukce RD Dr. Jánského, Chrudim\"", "", Rozhodnuti},
		{"ROZHODNUTÍ - povolení stavby: Tymákov,PM.čp.156 kVN DTS kNN", "", Rozhodnuti},
		{"Rozhodnutí o povolení záměru", "", Rozhodnuti},
		{"Rozhodnutí - Povolení záměru Dostavba kanalizace a teplovod. připojení_obec Kovářov", "", Rozhodnuti},
		{"R Prodloužení platnosti stavebního povolení HODOUSICE VODOVOD", "", Rozhodnuti},
		{"Změna stavby před dokončením - NOVOSTAVBA BYTOVÉ DOMU V PROLUCE VČETNĚ PŘÍPOJEK", "", Rozhodnuti},
		{"rozhodnutí o záměru: Obnova vodovodu a kanalizace Plzeň, ul. Hruškova", "", Rozhodnuti},
		{"OS ÚSO ÚR o umístění stavby veř.vyhl.", "", Rozhodnuti},
		{"Rozhodnutí - Novostavba rodinného domu a bourání rodinného domu Travčice č.p. 18", "", Odstraneni},
		{"SMO/715731/26/Vnitř./Trv\nRozhodnutí o povolení odstranění stavby „Demolice areálu Sokolská\".", "", Odstraneni},
		{"KS P 1749/2026\nzávazné stanovisko pro povolení záměru „Instalace BESS pro MNV Ostrava-Vítkovice\".", "", Stanovisko},
		{"Veřejná vyhláška- PROJEDNÁNÍ UPRAVENÉHO NÁVRHU ZMĚNY Č. 4 ÚZEMNÍHO PLÁNU ÚSTRAŠICE", "", UzemniPlan},
		{"Návrh na zrušení části opatření obecné povahy: změna X/2A Územního plánu Olomouc pod č. 1/2026", "", UzemniPlan},
		{"Veřejná vyhláška - návrh Aktualizace č. 1 Zásad územního rozvoje Jihomoravského kraje", "", UzemniPlan},
		{"Oznámení o zahájení zjišťovacího řízení k záměru „EMEA DISTRIBUČNÍ CENTRUM“ dle zákona č. 100/2001 Sb.", "", EIA},
		{"Záměr prodat část pozemku p. č. 338/2 v k. ú. Háje u Příbramě", "", Majetek},
		{"Záměr propachtování části pozemku v k.ú. Tišnov - parc. č. 2279/1", "", Majetek},
		{"Záměr bezúplatného převodu nemovité věci v k.ú. Nové Hamry", "", Majetek},
		{"Záměr města pronajmout hmotnou nemovitou věc č. 33/2026 - část pozemku v k. ú. Domažlice", "", Majetek},
		{"Krajský pozemkový úřad České Budějovice, aukční vyhláška na prodej nemovitých věcí - pozemky v katastrálním území Dolní Radouň", "", Majetek},
		{"Stavební záměr: \"Fotovoltaika - ČS a VDJ Vypich, P6\", na pozemcích v k.ú. Břevnov", "", OstatniStav},
		{"Výstavba stanice metra DEPO Zličín", "", OstatniStav},
		{"Kolaudační rozhodnutí - Bytový dům Na Výsluní", "", Kolaudace},
		{"Povolení záměru stavby: „Podniková prodejna Mlékárna Stříbro s. r. o.“", "", Rozhodnuti},
		{"Veřejná vyhláška - oznámení zahájení společného řízení", "", Zahajeni},
		{"497 Oznámení o zahájení řízení - jednání (U+S): \"rozšíření veřejného osvětlení, ulice Topolská, Chrudim II\"", "", Zahajeni},
		{"Veřejná vyhláška - vyrozumění o zahájení řízení CETIN FTTX__RD_LIBLME001_Liblice", "", Zahajeni},
		{"Stavba D7 MÚK Aviatická", "", OstatniStav},
		{"Stavba pro reklamu na pozemku parc.č. 1240/4 k.ú. Holešovice", "", OstatniStav},
		{"Výzva žadateli, aby se vyjádřil k námitkám týkajících se záměru: Rozšíření parkoviště na ul. Alžírské", "", Zahajeni},
		{"Veřejná vyhláška - oznámení o zahájení projednání Návrhu Zprávy ÚP Buk", "", UzemniPlan},
		{"Žádost o povolení dělení nebo scelení pozemků", "", Zahajeni},
		{"VŘ referent odboru výstavby a územního plánování MěÚ - územně plánovací činnost PRODLOUŽENO", "", ""},
		{"VPS - TJ Tělovýchovná zařízení Rakovník, z. s.", "Stavební úřad", ""},
		{"Poskytnutí kopie rozhodnutí MHMP STR č.j. MHMP 1003279/2021 ve věci změny užívání", "", ""},
		{"MěÚ Kroměříž - veřejná vyhláška, stanovení přechodné úpravy provozu v rámci realizace stavby", "", ""},
		{"MěÚ Kroměříž, odbor stavebního úřadu - veřejná vyhláška, JES - Rekonstrukce Areálu na Hrázi DSP2", "", Stanovisko},
		{"SMKL/177303/2026/OV/3/RM Výst./177303/2026/330/RM Záměr: Z/2026/150902 Řízení: R/2026/191294", "", OstatniStav},
		{"oznámení o zahájení řízení - stanoviska (bez jednání): \" Čistá Berounka - II. etapa - ČOV a KANALIZA", "", Zahajeni},
		{"Vyjádření a stanoviska ke stavebním záměrům v oblasti Praha 9", "", Stanovisko},
		{"Vývěs-NÁVRH op.ob.povahy,trvalé DZ,úprava přednosti ul. Hřbitovní v obci Velký Šenov", "", ""},
		{"Poskytování informace dle zák. č. 106/1999 Sb.; (k. ú. Ďáblice) ; S-MHMP 31072/2025", "", ""},
		{"OŽP-Veřejná-vyhláška-o-doručování: mvn HORKA (Orlov)", "", ""},
		{"Návrh změny č. 4 ÚP Most - veřejné projednání", "", UzemniPlan},
		{"OOP - stanovení PDZ - ul. Leitnerova – stavební úpravy objektu OKOL", "", ""},
		{"Veř. vyhláška PDZ - Svitavské nábřeží, zábor pro zařízení stavby", "", ""},
		// not about building or land
		{"Výzva k odstranění vozidla z komunikace Rybalkova, Peugeot", "", ""},
		{"Upozornění k odstranění a okleštění stromoví a jiných porostů", "", ""},
		{"Rozhodnutí - povolení úplné uzavírky", "", ""},
		{"Oznámení o nálezu věci na území obce - 83/26 - jízdní kolo", "", ""},
		{"Rozpočtové opatření - Usnesení Zastupitelstva č.32/25", "", ""},
		{"Veřejná vyhláška - oznámení o uložení písemnosti", "", ""},
		{"Usnesení o rozhodnutí vyhlášení - stav nedostatku vody na území Jihočeského kraje", "", ""},
		{"Odpověď na žádost o informace dle zákona č. 106/1999 Sb. – změny v územním plánu a výškový limit k.ú. Kyje", "", ""},
		{"Obecně závazná vyhláška č. 2/2026 o místním poplatku", "", ""},
		// agenda decides when the title is bare
		{"Veřejná vyhláška", "Stavební úřad", OstatniStav},
	}
	for _, c := range cases {
		if got := Classify(c.title, "", c.agenda); got != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"Veřejná vyhláška pro: PETR KOVÁŘ, nar. 01.02.1990, Riga":                            "Veřejná vyhláška pro: [osoba], Riga",
		"MUDr. Šárka Nováková Dvořáková_praktický lékař":                                     "[osoba]_praktický lékař",
		"Novostavba RD, žadatel: Petr Svoboda, k.ú. Hrutov":                                  "Novostavba RD, žadatel: [osoba], k.ú. Hrutov",
		"stavebník ČEZ Distribuce, a.s., kVN":                                                "stavebník ČEZ Distribuce, a.s., kVN",
		"Výzva k odstranění vozidla_1AB 2345_Miroslav Dvořák":                                "Výzva k odstranění vozidla_1AB 2345_[osoba]",
		"Bytový dům na ulici Prokopa Velikého 390/15":                                        "Bytový dům na ulici Prokopa Velikého 390/15",
		"rozšíření ul. Jana Palacha":                                                         "rozšíření ul. Jana Palacha",
		"Rekonstrukce RD Dr. Jánského, Chrudim":                                              "Rekonstrukce RD Dr. Jánského, Chrudim",
		"Elektronická dražba na prodej nemovité věci v k.ú. Radobyčice, Povinný: Radek Malý": "Elektronická dražba na prodej nemovité věci v k.ú. Radobyčice, Povinný: [osoba]",
		"Žebětín - 748, Novostavba RD Kopeckovi, Povolení záměru R/2026/100000/1":            "Žebětín - 748, Novostavba RD [osoba], Povolení záměru R/2026/100000/1",
		"rodinný dům pro Svobodovy, k.ú. Hrutov":                                             "rodinný dům pro [osoba], k.ú. Hrutov",
		"pro manžele Novákovy, Hrutov 12":                                                    "pro manžele [osoba], Hrutov 12",
		"Ing. arch. Jan Dvořák, Ph.D. – projektant":                                          "[osoba] – projektant",
		"Rozhodnutí - povolení stavby: Petříkov - TS Škola TS Za drahou VN NN":               "Rozhodnutí - povolení stavby: Petříkov - TS Škola TS Za drahou VN NN",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
