# CyberSec Model — Humne Apna LLM Kaise Banaya (Hinglish Guide)

> Yeh document simple Hinglish mein samjhata hai ki humne Sanjivani University ke
> Cyber Security department ke liye **apna khud ka AI model "CyberSec"** kaise
> banaya, train kiya, aur platform mein deploy kiya. Real-life examples ke saath.

---

## 0. Sabse pehle — "LLM" hota kya hai? (2-minute intro)

LLM = **Large Language Model**. Yeh ek aisa program hai jo bahut saara text
padh ke "agla shabd kya aayega" predict karna seekh leta hai. Jaise mobile
keyboard "Good morning..." ke baad "sir" suggest karta hai — bas LLM usi cheez
ka bahut bada, bahut smart version hai.

**Real-life example:**
Socho ek naya intern aaya hai jisne zindagi mein crores kitaabein padhi hain,
par usko **aapke college ke rules bilkul nahi pata**. Woh general baat achhi
karega, par "period 1 kitne baje start hota hai?" poochho toh galat bol dega.
Humne exactly yahi problem solve ki — ek smart intern liya aur usko **apne
department ka gyaan** de diya.

---

## 1. Humein LLM ki zaroorat kyun padi? (Problem statement)

Humara platform **CyberRange OS** hai — students yaha Red Team (attack) aur
Blue Team (defend) practice karte hain. Isme AI copilot chahiye tha jo:

1. **Red Team** mein next hacking step suggest kare (jaise "ab nmap scan chalao").
2. **Blue Team** mein alert ko triage kare ("yeh SQL injection attack hai, true positive").
3. **Department Assistant** ban ke students ke routine/timetable ke sawaal ka jawab de.

Aur sabse important condition: **data bahar nahi jaana chahiye.** Agar hum
ChatGPT/OpenAI use karte, toh students ka data internet pe chala jaata. College
ke liye yeh privacy risk hai. Isliye humne decide kiya —
**apna model, apne server pe, fully local.**

**Real-life example:**
Yeh bilkul aisa hai jaise bank apna khud ka security guard rakhta hai jo sirf
bank ke andar kaam karta hai — bahar ki kisi company ko andar ki cheezein nahi
dikhti. Waise hi humara CyberSec model sirf college ke server pe chalta hai.

---

## 2. Poora banane ka roadmap (bird's eye view)

```
[1] Base model choose karo   →  (ready-made small model uthaya, zero se nahi banaya)
[2] Apna data collect karo   →  (cybersecurity + department ka data)
[3] Fine-tune / specialise    →  (SOC triage ke liye tuned build banaya)
[4] GGUF mein convert karo    →  (server pe chalane layak format)
[5] Local runtime pe chalao   →  (server pe model host kiya)
[6] Do model split kiya       →  (general + SOC-tuned)
[7] Department gyaan diya      →  (timetable, faculty, students knowledge base)
[8] Platform se connect kiya   →  (API gateway ke through)
[9] Deploy + optimize          →  (RAM mein pin, fast response)
```

Har step ko ab detail mein dekhte hain.

---

## 3. Step 1 — Base model choose karna (zero se nahi banaya!)

**Important reality check:** Apna LLM "zero se" banane ke liye crores rupaye,
hazaaron GPU, aur mahine lagte hain (GPT jaise). Ek college project mein yeh
sambhav nahi. Isliye **smart tareeka** yeh hai:

- Ek **chhota, already-trained open-source model** uthao (jo companies free mein
  dete hain), aur usko apne data pe **thoda aur train karo (fine-tune)**. Isko
  bolte hain **transfer learning**.

Humne base ke liye chhote open models use kiye:
- **llama3.2:3b** — general kaam (copilot, assistant) ke liye. "3b" matlab
  3 billion parameters — chhota, CPU pe bhi chal jaata hai.
- Fine-tune ke liye **Qwen2.5-1.5B-Instruct** — sirf 1.5 billion parameters,
  isliye ek free Google Colab GPU pe hi train ho gaya.

**Real-life example:**
Yeh aisa hai jaise aapko chef banna hai. Aap zero se cooking nahi seekhte —
aap ek **already-trained cook (base model)** lete ho jo basic khana bana leta
hai, aur usko sirf **"Maharashtrian cuisine" ki special training (fine-tune)**
dete ho. Ab woh cook Maharashtrian khaana expert ban jaata hai, baaki sab bhi
bana leta hai.

---

## 4. Step 2 — Apna data collect karna

Fine-tuning ke liye **examples** chahiye — "sawaal → sahi jawab" ke jode. Humne
do type ka data use kiya:

### (a) Cybersecurity training data (SOC triage examples)
Jaise:
```
Input:  "Alert: repeated SQL injection signatures from one IP to the web server. Triage as JSON."
Output: {"summary":"SQL injection attempt detected...","verdict":"true_positive","mitre":"T1190"}
```
Yeh hazaaron aise examples the — alert aaya, aur uska sahi triage kya hai.

### (b) Department ka data (Sanjivani CY department)
- `sanjivani_timetables.json` — 1st/2nd/4th year ka poora timetable, courses, faculty.
- Student data export — 127 students, PRN, year, section.
- sanjivani.edu.in se public info — address, contact, timings.

**Real-life example:**
Sochो aap kisi bachche ko ganit padha rahe ho. Aap usko sirf theory nahi dete —
aap **solved examples** dete ho: "2+2=4, 3+5=8..." Bachcha pattern samajh ke
naye sawaal khud solve karne lagta hai. LLM bhi exactly aise hi examples se
seekhta hai.

---

## 5. Step 3 — Fine-tuning (QLoRA) — asli "apna model" wala part

Yaha humne base model ko apne cybersecurity data pe train kiya. Technique use ki
**QLoRA** (Quantized Low-Rank Adaptation). Iska simple matlab:

- Poore model ko dobara train karna mehenga hai (crores parameters).
- QLoRA sirf ek **chhota "adapter" layer** train karta hai (model ke upar ek
  patli si extra parat), baaki model ko freeze kar deta hai.
- Isse ek **free Colab T4 GPU** pe hi, 1-3 ghante mein training ho jaati hai.

**Real-life example:**
Aapke paas ek expert doctor hai (base model). Aap usko dobara MBBS nahi
karwaate. Aap bas usko ek **short specialization course (adapter)** karwaate ho
— jaise "cyber-forensics ki 2-hafte ki workshop". Ab woh doctor us field mein
sharp ho gaya, par baaki saari medical knowledge waise ki waise hai. QLoRA
bilkul yahi karta hai — kam kharche mein, kam time mein, specialize.

**Practical steps jo humne kiye (Colab notebook `training/finetune_qlora.ipynb`):**
1. Base model `Qwen2.5-1.5B-Instruct` load kiya (4-bit mein, RAM bachane ke liye).
2. Apna `dataset.jsonl` (sawaal-jawab examples) diya.
3. QLoRA adapter train kiya (~1-3 ghante).
4. Adapter ko base model mein **merge** kiya → final specialized model bana.

---

## 6. Step 4 — GGUF format mein convert karna

Training ke baad model ek heavy Python format mein hota hai. Server pe fast
chalane ke liye humne usko **GGUF format** mein convert kiya + **quantize** kiya
(`q4_k_m`).

- **Quantize** matlab: model ke numbers ko chhota kar do (32-bit → 4-bit). Size
  ~940 MB reh jaata hai, aur **CPU pe bhi chal jaata hai** (GPU zaroori nahi).

**Real-life example:**
Yeh aisa hai jaise ek 4K HD movie (bahut badi file) ko aap **compress** karke
ek chhoti MP4 bana lete ho jo aapke normal phone pe bhi bina lag ke chalti hai.
Quality thodi kam hoti hai par kaam ho jaata hai — aur file bahut chhoti.

Result: `cybersec-soc.gguf` (~940 MB) — humara apna fine-tuned model, ek single
file mein, server pe copy karne layak.

---

## 7. Step 5 — Local runtime pe model chalana

Model ko server pe serve karne ke liye ek **local inference runtime** use kiya
(jo model ko RAM mein load karke API ke through jawab deta hai). Humne server pe
model register kiya:

```
FROM cybersec-soc.gguf
PARAMETER temperature 0.3
SYSTEM "You are CyberSec, tuned for SOC triage and MITRE ATT&CK."
```

Isse humara model `cybersec-soc` naam se server pe available ho gaya, port
`11434` pe, sirf **localhost** ke liye (internet ko exposed nahi).

**Real-life example:**
Model file (GGUF) ek **bina bijli ke fridge** jaisa hai — rakha hua hai par kaam
nahi kar raha. Runtime us fridge ko **plug-in** karta hai — ab woh chालu hai aur
"cheezein thandi karo (jawab do)" bol sakte ho.

---

## 8. Step 6 — Do model split kiya (ek zaroori engineering decision)

Yaha ek asli problem aayi jo humne solve ki (yeh interview/viva mein bolne wali
baat hai):

- Humara fine-tuned `cybersec-soc` model **sirf SOC triage JSON** pe trained tha.
- Jab humne usse **Red Team copilot** ka alag format maanga (jaise "nmap command
  suggest karo JSON mein"), toh woh **khaali jawab** de raha tha — kyunki woh
  format usne kabhi seekha hi nahi tha. Result: 503 error.

**Solution:** Humne 2 model rakh diye —

| Model | Kaam | Kyun |
|---|---|---|
| `cybersec` (general) | Red Team copilot, assistant, MITRE tagging, reports | General model har tarah ka JSON format samajh leta hai |
| `cybersec-soc` (fine-tuned) | Sirf Blue Team SOC triage | Specialized model, sirf apne trained kaam mein use |

**Real-life example:**
Aapke paas do log hain — ek **general doctor (physician)** jo har chhoti-moti
problem dekh leta hai, aur ek **heart specialist (cardiologist)** jo sirf dil ka
expert hai. Agar aap cardiologist ko sardi-zukaam dikhaओge toh woh confuse ho
jaayega. Isliye general cases general doctor ko, aur heart ka case specialist
ko — bilkul waise hi humne model ka kaam baant diya.

---

## 9. Step 7 — Department ka gyaan dena (Knowledge Base / RAG)

Fine-tuning se model cybersecurity mein achha ho gaya, par usko **Sanjivani ka
timetable, faculty, students** yaad nahi the (yeh training data mein nahi the).

Har baar naya data aane pe pura model dobara train karna bewakoofi hai (mehenga
+ slow). Iski jagah humne **Knowledge Base** approach use ki:

- Humne aapke JSON files se ek **saaf-suthri department knowledge file** banaayi
  (`department_knowledge.md`) — HOD, faculty, period timings, har year ke courses,
  student count, university details.
- Yeh knowledge har sawaal ke saath model ko **context** mein diya jaata hai. Model
  usko padh ke exact jawab deta hai.

**Real-life example:**
Sochो ek naya receptionist hai jo smart toh hai par college ka schedule nahi
jaanta. Aap usko dobara "school" nahi bhejte — aap uske **desk pe ek notebook
rakh dete ho** jisme saara timetable, faculty naam, timings likhe hain. Ab jab
koi poochhta hai "period 1 kab hai?", woh notebook dekh ke turant sahi jawab
deta hai. Yeh notebook hi humara Knowledge Base hai.

**Verified result (live server pe test kiya):**
- "Who is the HOD?" → *Dr. Chetan Bawankar* ✓
- "Period 1 timing?" → *10:00–10:55 AM* ✓
- "How many students?" → *127 (106 in 3rd year, 18 in 4th, 3 in 2nd)* ✓

> Faculty/admin isko Assistant page se kabhi bhi edit kar sakte hain, aur reboot
> pe overwrite nahi hoga.

---

## 10. Step 8 — Platform se connect karna (LLM Gateway)

Model akela kuch nahi karta. Humne ek **LLM Gateway** banaya (Go API mein) jo
beech mein manager ki tarah kaam karta hai:

1. Request aayi (jaise "yeh alert triage karo").
2. Gateway decide karta hai **kaunsa model** use karna hai (SOC ke liye
   `cybersec-soc`, baaki ke liye `cybersec`).
3. Model se jawab leta hai, format check karta hai.
4. Har call ko **log** karta hai (audit ke liye), aur ek **egress guard** check
   karta hai ki model localhost pe hi hai (internet pe nahi).

**Real-life example:**
Gateway ek **office manager** jaisa hai. Aap manager ko kaam bolte ho, woh decide
karta hai ki yeh file kis employee (model) ko deni hai, kaam hone ke baad check
karta hai, aur register mein entry (log) karta hai. Aap directly employee se
deal nahi karte — sab manager ke through.

---

## 11. Step 9 — Deploy aur optimize

Server pe (Oracle Cloud, ARM64, 2 CPU, 10GB RAM) sab deploy kiya. Ek chhoti
problem aayi thi — **pehli request slow** aa rahi thi (model RAM mein load hone
mein 6-8 second lagte the), isliye assistant kabhi-kabhi 500 error deta tha.

**Fix:** Model ko **RAM mein permanently pin** kar diya
(`OLLAMA_KEEP_ALIVE=-1`) aur boot pe pehle se **warm-up** kar diya. Ab pehli
click bhi turant kaam karti hai.

**Real-life example:**
Yeh aisa hai jaise scooter thandi hai toh pehli kick mein start nahi hoti — 2-3
kick lagti hain. Toh humne scooter ko **hamesha warm rakh diya (idle chalu)** —
ab jab bhi chalao, turant chalti hai, pehli kick mein.

---

## 12. Final architecture — sab kaise judta hai

```
Student (browser)
      │  "yeh alert kya hai?" / "period 1 kab hai?"
      ▼
Web app (Next.js)  ──▶  Go API + LLM Gateway
                              │  (kaunsa model? SOC ya general?)
                              ▼
                    ┌──────────────────────────┐
                    │  CyberSec model (local)   │
                    │  cybersec  +  cybersec-soc │
                    │  + department knowledge   │
                    └──────────────────────────┘
                              │
                    jawab wapas student ko
```

Sab kuch **college ke server pe** — koi data bahar nahi.

---

## 13. Ek line mein poora summary (viva/presentation ke liye)

> "Humne zero se model nahi banaya — humne ek chhota open-source model liya,
> usko **QLoRA se cybersecurity data pe fine-tune** kiya, **GGUF mein quantize**
> karke apne college server pe **fully local** host kiya. General kaam ke liye
> ek model, SOC triage ke liye specialized fine-tuned model. Aur Sanjivani CY
> department ka poora timetable/faculty/student data ek **knowledge base** ke
> through diya, taaki model humare department ke baare mein sab kuch jaanta hai —
> aur student ka koi data internet pe nahi jaata."

---

## 14. Technical terms — chhoti dictionary (yaad rakhne ke liye)

| Term | Simple matlab |
|---|---|
| **LLM** | Text samajhne/likhne wala AI model |
| **Base model** | Ready-made trained model jise hum aage tune karte hain |
| **Fine-tuning** | Base model ko apne data pe thoda aur train karna |
| **QLoRA** | Sasta + fast fine-tuning (sirf chhota adapter train hota hai) |
| **Parameters (3b, 1.5b)** | Model ke "dimaag" ke connections — zyada = smart par bhaari |
| **Quantization (q4)** | Model ko chhota karna (32-bit → 4-bit) taaki CPU pe chale |
| **GGUF** | Model ka compressed file format, ek hi file mein |
| **Inference runtime** | Model ko chalane/serve karne wala software |
| **Knowledge Base / RAG** | Model ko live "notebook" dena jisme fresh info hoti hai |
| **LLM Gateway** | Manager jo decide karta hai kaunsa model, aur log rakhta hai |
| **Local inference** | Model apne server pe chalta hai, cloud/internet pe nahi |
| **Egress guard** | Safety check — model bahar internet pe na jaaye |

---

_Ready for viva/demo. Koi bhi step pe aur detail chahiye toh bol dena — main us
part ko aur simple ya aur technical bana sakta hoon._
