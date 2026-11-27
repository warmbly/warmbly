# AgentRequest işleri

Paket `internal/pkg/generation`. İşaret `- [ ]` açık, `- [x]` bitti. Kutusu olan her satır kapanır. Kod satırı olmayan madde yalnız testtir.

Bu paket sağlayıcının ne garanti ettiğini kilitler. Onay politikası, kredi kesme ve Remie metni burada yazılmaz. `Approve` ve `PreIteration` çağıranın hook'udur. Test hook'u kurar, ürün kuralı eklemez.

Testler `httptest` ile kalır. Postgres, Redis ve ağ yok. Yeni testi bu paketin mevcut fixture'ına ekle: `writeAgentResponse`, `agentFixtureProvider`, `writeAgentFixture`, `catalogTestTools`. Bu yardımcılar yetmezse gövdeyi elde yaz. `writeAgentResponse` usage olarak yalnız `total_tokens` yazar, `finish_reason` koymaz ve tool çağrısını tek parçada gönderir. Bitince `gofmt -w` ile dokunulan Go dosyalarını yaz.

Bölüm 1 bugünkü kodun kilididir. Test kırmızıysa kodu eğme. Bölüm 2 tek davranış değişikliğidir. Onun testi düzeltmeden önce kırmızı, sonra yeşil olur.

Yapılmayacaklar:

- Anthropic yoluna SSE, `EventTextDelta`, `adaptParams` veya `web_search` server aracı eklemek
- `GenerationClient` ile `Provider` döngüsünü birleştirmek
- `needsKey` alanını davranışa bağlamak (alan okunmuyor, öyle kalsın)
- OpenAI `TokensUsed` hesabını girdi+çıktı toplamına çevirmek. Yorum satırı toplam der, kod `total_tokens` yazar
- Onay duruşundan önce giden OpenAI metin olayını susturmak
- Boş `Model` görünce `ModelForTier(true)` seçmek

## 1. Mevcut sözleşmeyi kilitle

### AR-01 Boş cevap

- [x] Test: `agent_final_response_test.go` içindeki `TestAgentRejectsEmptyAnswer`

Üç biçim de (`openai`, `openai-stream`, `anthropic`) `"  "` dönünce ve `ReserveFinalResponse` açıkken:

- `result` nil değil
- hata dolu ve metni `empty agent response` içerir
- `StopReason` `error`
- `Iterations` 1
- `Messages` yalnız kullanıcının ilk mesajıdır
- `Pending` nil

### AR-02 Erken cevap son tur metnini taşımaz

- [x] Test: `TestAgentReturnsEarlyAnswerWithoutSpendingReservedTurn`

`ReserveFinalResponse` açık, `MaxIterations` bu testte 0 kalır. İlk cevap düz metindir, tek istek gider. `MaxIterations` 1 yapılırsa ilk tur son tur olur ve cümle gelir. Ham gövde `Do not invent findings` içermez. `StopReason` `stop` kalır.

### AR-03 Yedeksiz son tur metni `stop`

- [x] Test: yeni `TestAgentTextOnLastIterationWithoutReserveStops`

`MaxIterations` 2, `ReserveFinalResponse` kapalı. İlk cevap bir araç çağrısı, ikinci cevap düz metin. İlk cevap da metinse ikinci istek hiç gitmez.

- modele giden istek 2
- her iki istekte de araç listesi boş değildir
- `StopReason` `stop`, `max_iterations` değil
- `Text` ikinci metindir

`openai`, `openai-stream`, `anthropic`.

### AR-04 Varsayılan sınırlar

- [x] Test: yeni `TestAgentDefaultsIterationsAndTokens`

`MaxIterations` ve `MaxTokens` sıfır. Model her tur araç ister. Araç listede olmayabilir. Döngü yine sürer.

Taze provider:

- modele giden POST 12
- `Iterations` 12
- `StopReason` `max_iterations`
- `Text` boş
- OpenAI gövdesinde `max_tokens` 2048 vardır, `max_completion_tokens` yoktur
- Anthropic gövdesinde `max_tokens` 2048 vardır

OpenAI akışsız yeter. `OnEvent` nil kalsın.

### AR-05 RunAgent sıcaklık ve tool_choice

- [x] Test: yeni `TestRunAgentOmitsTemperature`

Araç varken:

- OpenAI ve Anthropic gövdesinde `temperature` anahtarı yok
- OpenAI `tool_choice` `auto`
- Anthropic gövdesinde `tool_choice` yok

Araç yokken OpenAI gövdesinde `tool_choice` anahtarı da yok. `Complete` ayrıdır, AR-12.

### AR-06 Bitiş nedeni yok sayılır

- [x] Test: yeni `TestAgentFollowsToolCallsNotFinishReason`

`OnEvent` nil. OpenAI düz JSON, `finish_reason` choice üzerindedir.

- `finish_reason` `stop` ve `tool_calls` dolu: handler çalışır
- `finish_reason` `tool_calls` ve çağrı listesi boş, metin dolu: `StopReason` `stop`, handler çalışmaz

Anthropic:

- `stop_reason` `end_turn` ve bir `tool_use` bloğu: handler çalışır
- `stop_reason` `tool_use` ve yalnız `text`: `StopReason` `stop`, handler çalışmaz

### AR-07 Bilinmeyen araç ve handler hatası

- [x] Test: yeni `TestAgentFeedsToolFailureBackAndContinues`

İki tur. İlk turda iki çağrı, sıra bu: adı listede olmayan araç, sonra handler'ı `errors.New("boom")` dönen `RiskRead` araç. İkinci tur düz metin `Done`. `Approve` tanımlıdır.

`result.Messages` üzerinde, çağrı sırasıyla:

- döngü durmaz
- ilk tool içeriği `unknown tool` içerir
- ikinci tool içeriği `boom` içerir
- `Approve` 0 kez çalışır
- `StopReason` `stop`
- `Text` `Done`

İkinci HTTP isteğinin teli:

- OpenAI: iki ayrı `role: tool` mesajı
- Anthropic: tek `user` mesajında iki `tool_result` bloğu, aynı sırada

`openai` ve `anthropic`. Akış şart değil.

### AR-08 Aynı turda ikinci çağrı onaya takılır

- [x] Test: yeni `TestAgentApprovalStopsBeforeAnyHandler`

Model bir turda iki `RiskWrite` çağrı ister. İkisi de listededir. `Approve` ilkinde `nil`, ikincisinde `ErrApprovalRequired`.

- ilk çağrı onaylansa da handler sayısı 0
- `RunAgent` hatası nil, `result` nil değil
- `StopReason` `approval_required`
- `Iterations` 1
- `Pending.Call` ikinci çağrıdır, `Pending.Risk` `write`
- `Messages` asistan turunu içermez

OpenAI ve Anthropic.

### AR-09 Approve başka hata

- [x] Test: yeni `TestAgentApproveErrorAborts`

`Approve` `errors.New("nope")` döner. Bu `ErrApprovalRequired` sarmalı değildir.

- handler çalışmaz
- dönen hata `nope` içerir
- `result` nil değil
- `StopReason` `error`
- `Iterations` 1
- `Pending` nil
- telde asistan turu yok

OpenAI ve Anthropic.

### AR-10 Onaydan önce giden OpenAI metni

- [x] Test: yeni `TestOpenAIEmitsTextBeforeApprovalPause`

`OnEvent` dolu. Model tek parçada `Looking` metni ve bir `RiskWrite` çağrı döner. `Approve` `ErrApprovalRequired` döner.

Olay sırası: `iteration`, bir kez `text_delta` `Looking`, `text` `Looking`. `tool_start` yok. `StopReason` `approval_required`. `Messages` asistan turunu içermez.

Aynı fixture Anthropic'te yalnız `iteration` üretir. `text` ve `text_delta` yoktur.

Bu madde davranışı değiştirmez.

### AR-11 Token toplamı

- [x] Test: yeni `TestAgentTokenSplit`

`OnEvent` nil. OpenAI usage: `prompt_tokens` 3, `completion_tokens` 4, `total_tokens` 9. Bir tur, düz metin.

- `TokensUsed` 9
- `InputTokens` 3
- `OutputTokens` 4

Anthropic usage: `input_tokens` 3, `output_tokens` 4.

- `TokensUsed` 7
- `InputTokens` 3
- `OutputTokens` 4

`writeAgentResponse` bu sayıları yazmaz. Gövdeyi elde kur.

### AR-12 Complete sıcaklığı ve uyarlama

- [x] Test: `openai_adapt_test.go` içine `TestCompleteAndCompleteStreamAdapt`

`Complete` araç göndermez. `reasoning_effort` reddi bu metotta beklenmez. Onu `TestCompleteAdaptsEveryRejectedParam` kilitler. `Complete` ve `CompleteStream` ayrı provider kullanır. Biri diğerinin yapışkan bayrağını devralmaz.

Sunucu sırayla `max_tokens` ve `temperature` reddeder, `stream_options` reddetmez, sonra içeriği `ok` olan gövde döner. `MaxTokens` 64, sıcaklık 0.7.

- her iki metot da `ok` döner
- reddedilene kadar sıcaklık 0.7 gider
- başarılı gövdede `max_completion_tokens` 64 vardır, `temperature` yoktur
- aynı provider üzerinde ikinci çağrı tek HTTP isteği atar

### AR-13 stream_options yapışkan bayrağı

- [x] Test: yeni `TestCompleteStreamDropsRejectedStreamOptions`

Taze provider. Sunucu yalnız `stream_options` için 400 döner, `param` `stream_options`. Başarı gövdesi boş değildir, metin `ok`.

- ilk çağrıda iki HTTP isteği: ilkinde `stream_options` vardır, ikincisinde yoktur
- sonraki `CompleteStream` tek istek atar ve alanı yine göndermez

### AR-14 Akış JSON'a düşer

- [x] Test: yeni `TestCompleteStreamAcceptsPlainJSON`

İstek `stream: true` gider. Cevap `Content-Type: application/json`, bir choice, içerik `ok`.

- `onDelta` bir kez `ok` alır
- dönüş metni `ok`
- hata yoktur

### AR-15 Parçalı tool çağrısı

- [x] Test: yeni `TestRunAgentStitchesStreamedToolArguments`

`OnEvent` dolu olmalıdır. Nil ise `RunAgent` SSE okumaz. `ReserveFinalResponse` kapalı. `writeAgentResponse` kullanma, o tek parça yazar.

İlk cevap iki `data:` çerçevesi, aynı `index` 0. Birinde `id` ve `name`, argüman `{"a":` ve `1}`. İkinci cevap da SSE, düz metin `Done`.

- handler bir kez çalışır ve argümanı `{"a":1}` görür
- `StopReason` `stop`

### AR-16 Çekirdek 87'de kesilir

- [x] Test: `tool_catalog_test.go` içine `TestToolCatalogCapsCoreAtEightySeven`

Kuruluş: `catalogTestTools(101)`, son araç `Deferred`. İlk 100 araç `Deferred` değil. 101 araç `Deferred` olmazsa katalog açılmaz, çünkü 101, 128'in altındadır. Hepsi `Deferred` değil ve sayı 128'i aşarsa çekirdek boş kalır. Bu madde o dal değildir.

Sınır sayısı 87'dir: `128 - 40 - 1`. Test bu sayıyı kodun kendi ifadesinden türetmez, 87 yazar.

`tools[87]` üzerine ayırt edici bir `InputSchema` koy.

- ilk `definitions` uzunluğu 88: `tool_0000` … `tool_0086` ve sonda arama aracı
- `tool_0087` ve `tool_0100` ilk listede yok
- `tool_0087` araması onu bulur
- sonraki `definitions` uzunluğu 89: çekirdek yerinde, bulunan araç `loaded` içinde, şema `DeepEqual`, sonda arama aracı

### AR-17 Resolve ve AI_FREE

- [x] Test: yeni `resolve_test.go`

`httptest` gerekmez. `Resolve`, sonra maddenin dediği yerde `NewProvider`.

- provider boş: adres `https://api.openai.com/v1`, `Local` false
- ` ollama ` ve `local`: anahtar boşsa anahtar `local`, adres `http://localhost:11434/v1`, iki tier da `llama3.1`, `Local` true, `IsLocal` true, `Name` `openai`
- `ollama`, `Free` false işaretçisi, anahtar boş: `Local` false, `OpenAIAPIKey` boş. `NewProvider` `ErrProviderNotConfigured` döner
- `ollama`, anahtar dolu, `BaseURL` dolu: adres verilen adrestir, `Local` true, anahtar değişmez
- `ollama`, `Model` `m`, `ModelTrial` `t`, `ModelPaid` `p`: trial `t`, paid `p`. `ModelPaid` boşsa paid `m` olur
- `openrouter`, `Free` nil: model `meta-llama/llama-3.1-8b-instruct:free`, `Local` false
- `groq`: adres `https://api.groq.com/openai/v1`, model `openai/gpt-oss-20b`, `Local` false
- `openai`, anahtar dolu, `Free` true: `Local` true, adres yine `https://api.openai.com/v1`, anahtar `local` yapılmaz
- `openai`, anahtar boş: `Resolve` hata dönmez, `OpenAIAPIKey` boş kalır, `NewProvider` `ErrProviderNotConfigured` döner
- `anthropic`, anahtar, `Model`, `BaseURL` ve `Free` true dolu: config'te yalnız `AnthropicAPIKey` ve verilen `Search` kalır. OpenAI alanları ve `Local` sıfırdır. `IsLocal` false, `Name` `anthropic`. Model ve adres yok sayılır
- `anthropic`, anahtar boş: `Resolve` hata dönmez, `NewProvider` `ErrProviderNotConfigured` döner
- `custom` ve `openai-compatible`, adres boş: hata, metin o provider adını içerir
- bilinmeyen ad, adres boş: aynı hata
- bilinmeyen ad, adres dolu: OpenAI uyumlu config, o adres, `Local` false
- `ProviderConfig` üzerinde iki anahtar birden: `NewProvider` OpenAI döner. Bu `Resolve` ile kurulmaz
- ikisi de boş: `ErrProviderNotConfigured`

### AR-18 Anthropic katalog, şema, onay, devam

- [x] Test: `anthropic_tools_test.go`

138 araç, hiçbiri `Deferred` değil. Hedefin `InputSchema` içinde `required` dursun. Bunlar ayrı koşulardır, tek turda birleştirilmez. OpenAI testleri olduğu gibi kalır.

Keşif, üç istek:

1. `tools` uzunluğu 1. Arama talimatı `system` alanındadır, `messages[0]` içinde aranmaz.
2. Tool sonucu, sonraki isteğin `user` mesajındaki `tool_result` içeriğindedir ve orijinal `required` listesini taşır. Hedef araç `input_schema` ile durur, şema orijinal map ile `DeepEqual`. `parameters` diye bakma.
3. Hedefin tool sonucu handler metnidir.

Ayrıca:

- `RiskRead` için `Approve` 0, `RiskWrite` ve `RiskSend` için 1. Arama aracı `RiskRead` olduğu için sayıma girmez
- `ErrApprovalRequired`: handler çalışmaz, `StopReason` `approval_required`, `Pending` hedef çağrıdır, telde asistan turu yoktur
- teste, çağıran gibi asistan turu ve tool sonucu eklenip ikinci `RunAgent` kurulur. O isteğin `tools` listesi hedef aracı yine taşır
- hiçbir Anthropic araç listesinde `type` `web_search_20250305` yoktur

### AR-19 Anthropic Complete tek atış

- [x] Test: yeni `TestAnthropicCompleteSendsTemperatureOnce`

`Complete`, `Temperature` 0 ile:

- gövdede `temperature` 0 vardır. Nil olmadığı için 0 silinmez
- araç listesi yoktur

Aynı metot 400 alınca ikinci istek atmaz. Hata döner. `adaptParams` bu yolda yoktur.

### AR-20 Boş model tier seçmez

- [x] Test: yeni `TestRunAgentEmptyModelUsesFreeDefault`

`req.Model` boş.

- OpenAI provider'da `modelTrial` `trial-x`, `modelPaid` `paid-y` ise giden model `trial-x`. Varsayılan sabitlerin ikisi de `gpt-6-luna` olduğu için bu fark struct alanıyla kurulur
- Anthropic giden model `claude-haiku-4-5`
- `ModelForTier(true)` Anthropic'te `claude-sonnet-4-6` döner ve `RunAgent` onu kendiliğinden seçmez

### AR-21 Telde olmayan araç aynı turda çalışır

- [x] Test: yeni `TestAgentRunsKnownToolMissingFromTheWire`

138 araç, hiçbiri `Deferred` değil. İlk istekte yalnız arama aracı vardır. Model aynı cevapta önce aramayı, sonra hedefi çağırır. Hedef `RiskRead`.

- ilk istekte hedef yoktur
- hedef handler o turda bir kez, verilen argümanla çalışır
- sonraki istekte hedef, orijinal şemasıyla listelenir

OpenAI'de şema alanı `parameters`, Anthropic'te `input_schema`.

## 2. Son tur, katalog talimatını düşürür

Bugün katalog açıldıysa `system` metnine arama talimatı eklenir. Son tur o stringin üstüne `agentFinalResponseInstructions` yazar ve araçları siler. İstek hem "aracı ara" hem "araç yok" der.

Düzeltme: son turda sistem metni `req.System + agentFinalResponseInstructions` olur. Katalog cümlesi eklenmez. Çağıranın kendi sistem metni durur. Son tur olmayan turlarda katalog cümlesi durur. Araç listesi son turda boş kalır. Beklenmeyen tool çağrısı yine çalıştırılmaz. `PreIteration` son turdan önce de çalışır.

Kırmızı sinyal, sistem metnindeki `Only some of your tools are listed` cümlesidir. Bu cümle bugün son turda vardır, düzeltmeden sonra yoktur. Arama aracının adı mesajlarda kalır. Tüm gövdede `search_available_tools` aramak hem bugün hem düzeltmeden sonra kırmızıdır. Onu kullanma.

- [x] Kod: `openai_provider.go` `RunAgent`, `finalTurn` dalı
- [x] Kod: `anthropic_provider.go` `RunAgent`, `finalTurn` dalı
- [x] Test: yeni `TestAgentFinalTurnDropsCatalogInstructions`

129 araç, hiçbiri `Deferred` değil. `MaxIterations` 2, `ReserveFinalResponse` açık. Sistem metni `MARKER-caller-system`. İlk cevap arama aracını çağırır. İkinci cevap düz metin.

İlk istek:

- `tools` uzunluğu 1
- sistem metni `Only some of your tools are listed` içerir

İkinci istek:

- `tools` uzunluğu 0
- sistem metni `MARKER-caller-system` ve `Do not invent findings` içerir
- sistem metni `Only some of your tools are listed` içermez
- mesajlarda arama cevabının `total_matches` alanı durur
- `PreIteration` 1 ve 2 ile çağrılmıştır

OpenAI'de sistem metni, sistem mesajının içeriğidir. Anthropic'te `system` alanıdır. `openai`, `openai-stream`, `anthropic`.

Tek araçlı mevcut son tur testleri yeşil kalır. Katalogları yoktur.

## Bitti

- [x] Bölüm 1 testleri bugünkü kodda yeşil
- [x] Bölüm 2 kodu yazıldı, testi yeşil
- [x] `gofmt -l` dokunulan Go dosyalarında boş
- [x] `go test ./internal/pkg/generation/` geçer
