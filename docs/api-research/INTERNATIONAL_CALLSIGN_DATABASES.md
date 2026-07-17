# International Amateur Radio Callsign Databases Research

Research into publicly downloadable amateur radio callsign databases and aggregated sources.

## Downloadable National Databases

| Country | Regulatory Body | Download URL | Format | Fields Included | Update Frequency | Record Count (Approx) | Restrictions |
|---------|-----------------|--------------|--------|-----------------|------------------|-----------------------|--------------|
| **USA** | FCC | [fcc.gov/uls](https://www.fcc.gov/wireless/systems-utilities/universal-licensing-system/uls-datasets) | ZIP (DAT/Pipe-delimited) | Callsign, Name, Address, License Class, Dates | Weekly (Full), Daily (Delta) | 750,000+ | None (Public Domain) |
| **Canada** | ISED | [ised-isde.canada.ca](https://ised-isde.canada.ca/site/amateur-radio-operator-certificate-services/en/downloads) | ZIP (Text/CSV) | Callsign, Name, Address, Proficiency (Basic/Morse/Adv), Status | Dynamic/Frequent | 70,000+ | None |
| **UK** | Ofcom | [ofcom.org.uk](https://www.ofcom.org.uk/manage-your-licence/radiocommunications/amateur-radio) | CSV | Callsign, License Type, Status (Name/Address often redacted for GDPR) | Periodic | 75,000+ | GDPR Redaction |
| **Germany** | BNetzA | [data.bundesnetzagentur.de](https://data.bundesnetzagentur.de/Bundesnetzagentur/SharedDocs/Downloads/DE/Sachgebiete/Telekommunikation/Unternehmen_Institutionen/Frequenzen/Amateurfunk/Rufzeichenliste/rufzeichenliste_afu.pdf) | PDF (Primary) | Callsign, Name, Address, Location | Monthly | 65,000+ | Primarily PDF; manual scraping needed for structured data |
| **Japan** | MIC | [motobayashi.net](http://motobayashi.net/callsign/licensesearch.html) | CSV | Callsign, Name, Location, Class, Expiration | Annual (via JJ1WTL) | 338,000+ (2025) | Official MIC search is online-only; bulk data via JJ1WTL |
| **Australia** | ACMA | [data.gov.au](https://data.gov.au/data/dataset/acma-register-of-radiocommunications-licences) | CSV/ZIP | Callsign, Licensee, Address (if public), Class, Location | Weekly | 15,000+ (Amateur subset) | Part of larger Spectrum dataset |
| **France** | ANFR | [data.anfr.fr](https://data.anfr.fr/tl/dataset/radioamateurs) | CSV | Stats, Dept, Category, Age Range, Geo (Long/Lat) | Annual | 13,000+ | Statistical focus; specific PII (Name/Address) often restricted |
| **Netherlands**| Rijksinspectie (RDI) | [rdi.nl](https://www.rdi.nl/documenten/publicaties/2023/10/01/overzicht-amateur-radiostations) | PDF/Online Search | Callsign, Class, Status | Periodic | 12,000+ | Limited bulk download (mostly PDF lists) |
| **Brazil** | ANATEL | [dados.gov.br](https://dados.gov.br/dados/conjuntos-dados/estacoes-de-radioamador) | CSV (Open Data) | Callsign, Name, Address, Class, State | Periodic | 40,000+ | Open Data Portal |
| **Mexico** | IFT | [bit.ift.org.mx](https://bit.ift.org.mx/Bitacora/RegistroPublicoConcesiones) | CSV/Online | Callsign, Licensee, Dates | Periodic | 3,000+ | Searchable "Registro Público de Concesiones" |

## Grouped by Implementation Strategy

### 1. CSV/Direct Text-Based (Easiest Integration)
*   **USA (FCC):** Pipe-delimited text files in ZIP. Very high volume but stable schema.
*   **Canada (ISED):** CSV/Text files in ZIP.
*   **Australia (ACMA):** Weekly CSV via data.gov.au.
*   **Japan (JJ1WTL):** Annual CSV export of MIC data.
*   **Brazil (ANATEL):** Open Data CSV files.
*   **France (ANFR):** Statistical CSVs available on Open Data portal.

### 2. PDF-Only/Restricted (Requires Processing/Scraping)
*   **Germany (BNetzA):** Official "Rufzeichenliste" is a massive PDF (600+ pages). Requires PDF parsing scripts.
*   **Netherlands (RDI):** Frequently published as PDF lists or searchable-only portals.

### 3. API-Only / Online Search (Requires Crawling/API Access)
*   **Spain (CNMC):** Online registry search.
*   **Mexico (IFT):** Web-based registry (BIT).
*   **UK (Ofcom):** Search portal is the primary up-to-date source; bulk datasets are less frequent.

## Aggregated/Unified Sources (Beyond QRZ/HamQTH)

1.  **QRZCQ.com:** Massive international database, often used as an alternative for non-US lookups. 198,000+ users.
2.  **RadioID.net:** The primary source for DMR (Digital Mobile Radio) IDs, mapped to callsigns. Downloadable JSON/CSV database.
3.  **eQSL.cc / LoTW (ARRL):** While verification systems, they maintain large databases of active users. Access usually requires account/API.
4.  **HamCall.net (Buckmaster):** Long-standing commercial alternative to QRZ.
5.  **Dxmaps.com:** Real-time international call lookup based on activity spots.
6.  **RadioReference.com:** Primarily scanning-focused but includes amateur radio license data in their unified database.
7.  **APRS.fi:** Real-time tracking of active amateur stations using APRS; good for coordinate/location mapping of active users.

## Recommendations for Implementation
*   **Prioritize USA and Canada** due to data richness and ease of parsing.
*   **Use RadioID.net** for mapping digital IDs to callsigns.
*   **Automate Australia (data.gov.au)** as it provides clean CSV data on a reliable schedule.
*   **Develop a PDF-to-CSV parser** specifically for Germany (BNetzA) due to its high activity level.
