# SwissTransfer CLI — spécifications

Version du document : 1.1 (2026-11-08), validé avec l'auteur. Basé sur la rétro-ingénierie de l'app web
SwissTransfer réalisée dans le projet `ik-api-explore` (protocole vérifié en conditions réelles
le 2026-11-08).

> **API non officielle.** Le CLI parle à l'application web SwissTransfer (Laravel + Inertia),
> pas à une API publique. Elle peut changer sans préavis : la couche HTTP est isolée dans un
> seul package (`internal/swisstransfer`) pour pouvoir la corriger vite.

---

## 1. Objectifs

| # | Objectif | Détail |
|---|---|---|
| O1 | Envoyer des fichiers/dossiers depuis un terminal | lien ou e-mail, mot de passe, durée, nb de téléchargements |
| O2 | Télécharger un transfert depuis son lien | avec ou sans mot de passe, tout ou partie des fichiers |
| O3 | Inspecter un transfert | liste des fichiers, tailles, expiration |
| O4 | Multi-plateforme | Linux, macOS, Windows (PowerShell), un binaire statique par OS/arch |
| O5 | Scriptable | stdout minimal et stable, `--json`, codes de sortie documentés, progression sur stderr |
| O6 | Zéro dépendance | Go, bibliothèque standard uniquement ; pas de navigateur, pas de runtime |
| O7 | Envoi anonyme | sans token, validation de l'adresse e-mail par code (comme sur swisstransfer.com) |
| O8 | Complétion shell | bash, zsh, fish, PowerShell |

### Non-objectifs (v1)

- Supprimer / prolonger un transfert (uniquement via liens signés reçus par e-mail).
- Historique des transferts (`/accounts/{account}/transfers` reste vide pour les transferts au token).
- Reprise d'un téléchargement interrompu (`Range` non testé) ; reprise d'un upload interrompu.
- Interface interactive (TUI).

---

## 2. Choix techniques

| Sujet | Décision | Pourquoi |
|---|---|---|
| Langage | **Go ≥ 1.22**, stdlib uniquement | cross-compilation triviale (`GOOS/GOARCH`), binaire statique unique, pas de supply chain |
| Nom du binaire | `swisstransfer` (`swisstransfer.exe` sous Windows) | |
| Dépôt / module | `github.com/kallioli/swisstransfert-cli` | |
| Langue des messages | anglais | projet open source ; stdout reste scriptable |
| Cibles de build | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64 | |
| Parsing CLI | `flag.FlagSet` par sous-commande | pas de dépendance ; aide générée à la main |
| Config | fichier JSON, permissions 0600 | lisible, stdlib |
| HTTP | `net/http` + `cookiejar`, redirections **non suivies** | détecter les 302 vers login/ksuite et produire une erreur claire |
| Concurrence | `ThreadPool` de goroutines, morceaux de 50 Mio | protocole S3 multipart imposé par le serveur |

---

## 3. Interface en ligne de commande

```
swisstransfer <commande> [options] [arguments]

Commandes :
  send        Envoyer des fichiers ou dossiers (token requis)
  download    Télécharger un transfert depuis son lien (alias : get)
  info        Afficher le contenu d'un transfert
  login       Enregistrer le token API (et le space id kSuite) dans la config
  logout      Supprimer les identifiants de la config
  config      Afficher / modifier la configuration (path, show, set, unset)
  completion  Générer le script de complétion (bash, zsh, fish, powershell)
  version     Afficher la version
  help        Aide

Options globales :
  --json          Sortie JSON sur stdout (une seule valeur, pas de progression)
  -q, --quiet     Pas de progression ni de messages d'information sur stderr
  -v, --verbose   Journaliser les requêtes HTTP sur stderr (le token est masqué)
  --config PATH   Fichier de configuration à utiliser
  --token T       Token API (prioritaire sur l'env et la config)
  --space-id N    Space id kSuite (idem)
```

### 3.1 `send`

```
swisstransfer send [options] CHEMIN...

  --to EMAIL            Destinataire (répétable ou séparé par des virgules). Active le mode e-mail.
  --from EMAIL          Envoi anonyme (sans token) : adresse de l'expéditeur, validée par un code reçu par e-mail
  --code CODE           Code de validation e-mail (sinon invite interactive)
  --title TEXTE         Titre du transfert
  --message TEXTE       Message (mode e-mail surtout)
  --password P          Mot de passe de téléchargement
  --password-stdin      Lire le mot de passe sur stdin (première ligne)
  --days N              Durée de validité en jours (défaut : 7 ; valeurs permises lues sur le serveur,
                        typiquement 1, 3, 7, 15, 30 ; 60 selon l'offre)
  --max-downloads N     Nombre max. de téléchargements, 1..250 (défaut : 250)
  --lang CODE           Langue des e-mails/pages : fr, en, de, it, es, nl, pt (défaut : en)
  --parallel N          Morceaux envoyés en parallèle (défaut : 4, max 16)
  --dry-run             Valider la demande côté serveur (Precognition) sans rien envoyer
```

Comportement :

- Sans `--to` → `method=link`. Avec `--to` → `method=email`, `recipients=[...]`.
- **Envoi anonyme** (`--from`, ou aucun token configuré) : le serveur envoie un code à l'adresse
  donnée ; le CLI attend le code sur le terminal (« Enter the code sent to … ») ou le lit dans
  `--code`, puis confirme et poursuit l'envoi. Détail du flux en §5.1 bis, à confirmer en test réel.
- `CHEMIN` peut être un fichier ou un dossier. Un dossier est parcouru récursivement ; le `path`
  envoyé au serveur est relatif au **parent** du dossier (`dossier/sous/fichier.ext`, séparateur `/`
  quel que soit l'OS). Les liens symboliques ne sont pas suivis. Les fichiers vides sont acceptés.
- Deux fichiers avec le même `path` relatif → erreur avant tout appel réseau.
- MIME : déduit de l'extension (`mime.TypeByExtension`), sinon `application/octet-stream`.
- **stdout** : uniquement l'URL de téléchargement (`https://www.swisstransfer.com/dl/<link_id>`).
- **stderr** : progression (voir §6).
- `--json` :
  ```json
  {"download_url": "...", "link_id": "...", "transfer_id": "...", "expires_at": "2026-11-15T20:41:00Z",
   "total_size": 123, "files": [{"id": "...", "path": "a.pdf", "size": 123, "mime_type": "application/pdf"}]}
  ```
- Ctrl-C ou erreur après création du transfert → `PATCH {"status":"cancelled"}` (best effort,
  délai 10 s), puis sortie 130 (interruption) ou 1.

### 3.2 `download` / `get`

```
swisstransfer download [options] LIEN

  -o, --output DIR      Dossier de destination (défaut : dossier courant)
  --file NOM            Ne télécharger que ce fichier (répétable ; `path` exact ou motif glob)
  --password P          Mot de passe du transfert
  --password-stdin      Lire le mot de passe sur stdin
  --force               Écraser les fichiers existants (défaut : renommage automatique `nom (1).ext`)
  --parallel N          Fichiers téléchargés en parallèle (défaut : 2, max 8)
```

- `LIEN` : URL complète (`https://www.swisstransfer.com/dl/<uuid>`, avec ou sans préfixe de
  langue, ou `swisstransfer.infomaniak.com/dl/<uuid>`) ou `<uuid>` nu.
- Aucune authentification nécessaire.
- Chaque fichier est écrit dans `DIR/<path>` (sous-dossiers créés), via un fichier temporaire
  `<nom>.part` renommé à la fin ; la taille est vérifiée. Si la cible existe déjà, le fichier est
  renommé `nom (1).ext`, `nom (2).ext`… sauf `--force` qui écrase.
- **stdout** : un chemin local par fichier écrit. `--json` : `{"link_id", "expires_at", "files": [{"path","size","local_path"}]}`.
- Lien protégé sans `--password` : si stdin est un terminal, invite « Password: » (saisie masquée
  si possible, sinon visible) ; sinon erreur, code 5.

### 3.3 `info`

```
swisstransfer info [options] LIEN
  --password P / --password-stdin
```

Sortie humaine : titre, expiration (date locale + délai restant), taille totale, tableau des
fichiers (taille, type, chemin). `--json` : objet `transfer` normalisé (identique à `download --json`
sans `local_path`).

### 3.4 `completion`

```
swisstransfer completion bash|zsh|fish|powershell
```
Écrit le script sur stdout. Exemples d'installation dans l'aide de la commande
(`source <(swisstransfer completion bash)`, `swisstransfer completion powershell | Out-String | Invoke-Expression`).

### 3.5 `login` / `logout` / `config`

```
swisstransfer login [--token T] [--space-id N] [--no-verify]
```
- Sans `--token` : invite sur stdin. Vérifie le token par un `GET /` et affiche l'utilisateur
  (`props.user.email`) ; si le serveur redirige vers kSuite, demande/suggère `--space-id`.
- Écrit la config (0600). `logout` supprime `token` et `space_id`.

```
swisstransfer config path            # chemin du fichier
swisstransfer config show            # contenu, token masqué (xxxx…1234)
swisstransfer config set CLÉ VALEUR  # clés : token, space_id, language, days, max_downloads, parallel
swisstransfer config unset CLÉ
```

---

## 4. Configuration et authentification

Ordre de priorité : **option de ligne de commande > variable d'environnement > fichier de config > défaut**.

| Clé config | Variable d'env | Rôle |
|---|---|---|
| `token` | `SWISSTRANSFER_TOKEN` (fallback `IK_API_TOKEN`) | Token API Infomaniak (manager → Développeur → Tokens API) |
| `space_id` | `SWISSTRANSFER_SPACE_ID` | Space kSuite ; requis pour un compte kSuite, sinon 302 |
| `language` | `SWISSTRANSFER_LANG` | défaut `en` |
| `days` | — | défaut 7 |
| `max_downloads` | — | défaut 250 |
| `parallel` | — | défaut 4 |
| — | `SWISSTRANSFER_CONFIG` | chemin du fichier de config |
| — | `SWISSTRANSFER_PASSWORD` | mot de passe (send/download/info) sans l'exposer dans l'historique |

Emplacement du fichier :

| OS | Chemin |
|---|---|
| Linux / macOS | `$XDG_CONFIG_HOME/swisstransfer/config.json`, sinon `~/.config/swisstransfer/config.json` |
| Windows | `%APPDATA%\swisstransfer\config.json` |

Le token n'apparaît jamais dans les logs (`--verbose` le masque) ni dans `config show`.

---

## 5. Protocole (rappel, référence `services/swisstransfer.md` du projet ik-api-explore)

### 5.1 Envoi — base `https://swisstransfer.infomaniak.com`

Un seul client HTTP avec cookie jar du début à la fin. Si `space_id` : `?ksuite-mode=main&space-id=N`
sur les pages (`GET /`, `POST /`).

1. `GET /` + `Authorization: Bearer` → cookies `SASESSION`, `SWISSTRANSFER-API-XSRF-TOKEN` ;
   extraire le JSON de `<script data-page="app" type="application/json">…</script>` →
   `version` (Inertia), `props.limits.expiry_days`, `props.limits.download_limit`, `props.user`.
   - 302 vers `login.infomaniak.com` → token invalide/absent (code 3).
   - 302 vers `ksuite.infomaniak.com` → `space_id` requis (code 3).
2. Headers des écritures : `Authorization`, `Accept: application/json`,
   `X-Requested-With: XMLHttpRequest`, `X-XSRF-TOKEN: urldecode(cookie)`.
3. `GET /api/1/altcha-challenge` → preuve de travail SHA-256 (`n ≤ maxNumber`, ≈ 1 M itérations max).
   `altcha = base64(json compact {algorithm, challenge, number, salt, signature, took})`.
4. (`--dry-run`) `POST /{lang}` + `Precognition: true`, corps identique avec `altcha: ""` → 204 / 422.
5. `POST /` + `X-Inertia: true` + `X-Inertia-Version` → `props.transfer{id, files[{id,path}]}`.
   409 → relire `version` via `GET /` et rejouer une fois. `props.errors` non vide → 422 logique.
6. Upload : `n = max(1, ceil(size/50 Mio))` ; `POST /api/1/transfers/{t}/files`
   `{"upload_url_requests":[{"file":id,"chunk":c}]}` par lots de **10 max**, `chunk=null` si `n==1`.
   `PUT url` (corps brut, `Content-Type: application/octet-stream`, sans Authorization) → `ETag` brut
   avec guillemets. 3 essais par morceau avec backoff ; redemander l'URL (expire après 1 h).
7. `PATCH /api/1/transfers/{t}/files/{f}` : `{}` si `n==1`, sinon `{"etags":[{"chunk_index":i,"etag":"\"…\""}]}`.
8. `PATCH /api/1/transfers/{t}` `{"status":"completed"}` → `data.link.download_url`.
   Abandon : `{"status":"cancelled"}`.

### 5.1 bis Envoi anonyme (vérifié en réel le 2026-11-08)

Base `https://www.swisstransfer.com`, session cookie initialisée par `GET /en`, sans `Authorization`.

- `POST /api/1/email-validations` `{"email"}` → 201 `{data:{id, token_expires_at}}` ; un code de
  6 caractères alphanumériques est envoyé par e-mail (sujet « Swisstransfer confirmation code: XXXXXX »).
  Erreur `email_validation_rate_limit_error` si trop de demandes.
- `POST /api/1/email-validations/{id}/confirm` `{"token":"XXXXXX"}` → 200 `{data:true}`.
  Erreurs : `invalid_validation_token` (mauvais code), `email_validation_request_locked`.
- `POST /api/1/email-validations/{id}/resend` : renvoyer le code.
- `GET /api/1/email-validations/{id}?email=…` → `{data:{is_validated}}` : permet de **réutiliser**
  un id déjà validé. Le CLI le met en cache dans la config (`email_validations`), et ne redemande
  un code que si `is_validated` est faux ou l'id refusé.
- Création : `POST /{lang}` (Inertia) comme en 5.1 avec `email: <adresse>` et
  `email_validation_id: <id>`. Réponse `step: "uploading"`, puis upload/finalisation identiques.
- Le `step: "cgu_acceptance"` de la page d'accueil est purement côté client (aucun appel serveur).

### 5.2 Téléchargement — base `https://www.swisstransfer.com` (sans auth)

1. `GET /dl/{link}` → page `link/show` (`props.transfer`) ou `link/password`.
   404 → lien inconnu/expiré (code 4).
2. Protégé : `POST /dl/{link}` `{"password":"…"}` + headers Inertia + XSRF → `link/show`.
   Toujours `link/password` ou `props.errors.password` → mauvais mot de passe (code 5).
3. Par fichier : `GET /api/1/links/{link}/files/{file}` → `data.url` (S3, 1 h) → `GET` en streaming.

### 5.3 Erreurs HTTP à gérer

| Statut | Signification | Réaction |
|---|---|---|
| 419 | CSRF | refaire `GET /` et rejouer une fois |
| 409 | version Inertia périmée | idem |
| 422 | validation (`errors{champ:[msg]}` ou `error.errors[]`) | afficher champ + message, code 1 |
| 429 | rate limit | attendre `Retry-After` (max 60 s), 3 essais |
| 5xx / réseau | | 3 essais avec backoff sur les appels idempotents |

---

## 6. Sortie, progression, codes de sortie

- **stdout** ne contient que le résultat (URL, chemins, JSON). Tout le reste va sur **stderr**.
- Progression (stderr, si c'est un terminal et sans `--quiet`/`--json`) : une ligne réécrite
  `\r` : `fichier.ext  312.5 MiB / 3.0 GiB  10%  48.2 MiB/s  ETA 57s`. Hors terminal : une ligne
  par fichier terminé.
- Messages d'erreur : `swisstransfer: <message>` sur stderr, en anglais, avec un conseil quand
  c'est possible (ex. « run `swisstransfer login` »).

| Code | Sens |
|---|---|
| 0 | succès |
| 1 | erreur générique (API, réseau, E/S) |
| 2 | usage (option ou argument invalide) |
| 3 | authentification (token absent/invalide, space id manquant) |
| 4 | transfert introuvable ou expiré |
| 5 | mot de passe requis ou incorrect |
| 130 | interrompu (Ctrl-C) |

---

## 7. Arborescence du projet

```
swisstransfert-cli/
├── cmd/swisstransfer/main.go        point d'entrée (version injectée par -ldflags)
├── internal/cli/                    sous-commandes, parsing, sortie
├── internal/swisstransfer/          client HTTP : session, altcha, create, upload, download
├── internal/config/                 chargement/écriture de la config, chemins par OS
├── internal/progress/               barre de progression stderr
├── scripts/build.sh, build.ps1      cross-compilation des 6 cibles dans dist/
├── .github/workflows/release.yml    build + release sur tag
├── SPECS.md, README.md, LICENSE (MIT)
```

---

## 8. Tests d'acceptation (exécutés le 2026-11-08 avec le binaire Go)

- [x] `send` d'un fichier de 0 octet, 1 Ko, 3 Ko et 120 Mio (3 morceaux) — SHA-256 identique après `download`
- [x] `send` d'un dossier → chemins relatifs conservés (`folder/sub/c.dat` visibles dans `info`)
- [x] `send --password` puis `info`/`download --password` ; mauvais mot de passe → code 5 ; absent → code 5
- [x] `send --to` (mode e-mail, 120 Mio) → e-mail reçu
- [x] `send --dry-run` → aucun transfert créé ; `--days 99` refusé avec la liste des valeurs permises (code 2)
- [x] Envoi anonyme `--from` : code 5 + e-mail de code, puis `--code` (mauvais → 5, bon → lien) ; 3e envoi sans code grâce au cache
- [x] Ctrl-C pendant l'upload → code 130, transfert passé en `cancelled` (PATCH 200)
- [x] Token sans space id → code 3 avec conseil ; `login --space-id` → « Logged in as … »
- [x] Lien inconnu → code 4 ; lien non-SwissTransfer → code 2
- [x] `download` d'un fichier déjà présent → `tiny (1).txt` ; `--file '*.dat'` ; `--password-stdin` ; `--json`
- [x] Complétion : `bash -n` et `zsh -n` OK ; fish et PowerShell générés (non testés sur machine)
- [x] Build des 6 cibles (ELF, Mach-O, PE32+) via `scripts/build.sh`
- [ ] Exécution réelle sous Windows/PowerShell (binaire compilé, non lancé)
- [ ] Fichiers > 3 Gio, reprise de téléchargement

Notes d'implémentation issues des tests :
- `POST /{lang}` avec `Precognition: true` fonctionne aussi sur `www.swisstransfer.com` ; `recipients` doit
  être `[]` et non `null`.
- Un mauvais mot de passe sur `POST /dl/{link}` renvoie un **302** vers la page (erreur flashée en session) :
  le CLI relit `GET /dl/{link}` et détecte `props.errors.password` ou le composant `link/password`.
- `GET /` sur `www.swisstransfer.com` redirige vers `/{lang}` : les redirections vers le même hôte sont suivies.
