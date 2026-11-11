# WowOfButtplug

Portage de l'addon [Butthurt](https://github.com/Maikatura/Butthurt) pour **World of Warcraft: Forever** (client 1.60.1, interface 16001), avec son propre pont Windows intégré (pas de .NET, rien à compiler).

Quand votre personnage prend des dégâts dans WoW, l'addon fait clignoter un petit pixel rouge dans le coin supérieur gauche de l'écran. Le pont `WowOfButtplug.exe` lit ce pixel et fait vibrer votre jouet (Lovense ou tout appareil compatible Buttplug) via **Intiface Central**.

``` 
WoW: Forever ──(pixel rouge)──> WowOfButtplug.exe ──(ws://localhost:12345)──> Intiface Central ──> votre jouet
```

> Un addon WoW ne peut pas ouvrir de connexion réseau (aucune API socket dans le jeu) : le petit programme natif est donc indispensable. Tout est fourni ici, il n'y a rien à compiler ni à aller chercher ailleurs.

## Ce dont vous avez besoin

- **World of Warcraft: Forever** (ou sa bêta) installé via Battle.net
- **Windows** pour le pont
- **Intiface Central** : <https://intiface.com/central/>
- Un jouet compatible (Lovense, etc.)

## Étape 1 — Intiface Central

1. Installez [Intiface Central](https://intiface.com/central/) (une seule fois).
2. Allumez votre jouet, ouvrez Intiface Central, cliquez sur **Start Server** et vérifiez que le jouet apparaît après un scan.

## Étape 2 — L'addon

1. Téléchargez ce dépôt (Code → Download ZIP) et décompressez-le.
2. Copiez le dossier `Butthurt` dans le dossier des addons du client **Forever** :
   ```
   C:\Program Files (x86)\World of Warcraft\_forever_\Interface\AddOns\Butthurt\
   ```
   Créez `Interface\AddOns` s'ils n'existent pas. C'est bien le dossier `_forever_`, pas `_classic_` ni `_retail_`.
3. En jeu, au choix des personnages, cliquez sur **AddOns** et vérifiez que **Butthurt** est coché (le TOC déclare l'interface `16001`, celle du client 1.60.1 : pas besoin d'activer « Charger les addons obsolètes »).

Un pixel noir de 5×5 px apparaît en haut à gauche de l'écran : c'est normal, c'est lui que lit le pont.

## Étape 3 — Jouer

1. Lancez WoW: Forever et connectez-vous à un personnage.
2. Double-cliquez sur **`Start.bat`** : il démarre Intiface Central si besoin, puis `WowOfButtplug.exe`, qui attend le serveur, connecte le jouet et affiche `Ready.`
3. **Gardez la fenêtre de WoW au premier plan** (le pont lit le pixel de la fenêtre active) et prenez des dégâts.

Pour arrêter : `Ctrl+C` dans la fenêtre du pont.

## Dépannage

- **`Could not reach the buttplug server`** : dans Intiface Central, cliquez **Start Server** (port 12345 par défaut). Le pont réessaie pendant 2 minutes avant d'abandonner.
- **`No vibration-capable device found`** : le jouet n'est pas appairé dans Intiface Central. Relancez un scan là-bas, puis `Start.bat`.
- **Rien ne se passe en jeu** : fenêtre de WoW pas au premier plan, ou pixel masqué par un autre élément d'UI en haut à gauche.
- **L'addon n'apparaît pas** : mauvais dossier (`_forever_`), ou le dossier ne s'appelle pas `Butthurt` avec le `Butthurt.toc` directement dedans.

## Différences avec la version d'origine (retail)

- **Le combat log n'existe pas sur le client Forever** : `COMBAT_LOG_EVENT_UNFILTERED` et `CombatLogGetCurrentEventInfo()` ne sont pas disponibles pour les addons. La détection de dégâts utilise donc une baisse de vie via `UNIT_HEALTH`.
- **Valeurs secrètes** : sur Forever, `UnitHealth` peut renvoyer une valeur « secrète » (règles Midnight) qu'il est interdit de comparer. L'addon détecte ce cas (`issecretvalue`) et bascule alors en mode dégradé : le pixel clignote à chaque changement de vie en combat (soins inclus).
- **Pas de Ace3** : l'addon est autonome (évite les problèmes connus d'AceDB sur ce client).
- **Pont remplacé** : `WowOfButtplug.exe` (Go, code source dans [`bridge/`](bridge/)) remplace `Butthurt.exe` — plus besoin du .NET Runtime ni de l'exécutable du dépôt d'origine. Il lit le pixel via l'API Win32 et parle les protocoles Buttplug v3 et v4 en WebSocket (négociation automatique : v3 demandé d'abord, repli v4 si le serveur le réclame), sans dépendance externe.

## Crédits

- Addon d'origine : [Maikatura/Butthurt](https://github.com/Maikatura/Butthurt)
- [Intiface Central](https://intiface.com/central/) / [Buttplug.io](https://buttplug.io)

Projet de fan, sans lien avec Blizzard Entertainment. Utilisez l'addon conformément aux règles du jeu.
