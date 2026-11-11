# WowOfButtplug

Portage de l'addon [Butthurt](https://github.com/Maikatura/Butthurt) pour **World of Warcraft: Forever** (client 1.60.1, interface 16001).

Quand votre personnage prend des dégâts dans WoW, l'addon fait clignoter un petit pixel rouge dans le coin supérieur gauche de l'écran. Un petit programme côté Windows (Butthurt.exe) lit ce pixel et fait vibrer votre jouet Lovense (ou tout appareil compatible Buttplug) via **Intiface Central**.

```
WoW: Forever ──(pixel rouge)──> Butthurt.exe ──(ws://localhost:12345)──> Intiface Central ──> votre jouet
```

## Ce dont vous avez besoin

- **World of Warcraft: Forever** (ou sa bêta) installé via Battle.net.
- **Windows** pour le pont Butthurt.exe (l'addon, lui, est indépendant de l'OS).
- **Intiface Central** : <https://intiface.com/central/>
- Un jouet compatible (Lovense, etc.).
- Le **.NET Runtime** si Butthurt.exe le réclame au lancement.

## Étape 1 — Installer Intiface Central et connecter le jouet

1. Téléchargez et installez [Intiface Central](https://intiface.com/central/).
2. Allumez votre jouet (Bluetooth ou USB).
3. Dans Intiface Central, cliquez sur **Start Server** (le serveur écoute par défaut sur `ws://localhost:12345`, c'est l'adresse qu'utilise Butthurt.exe).
4. Lancez un **Device Scan** et vérifiez que votre jouet apparaît dans la liste des appareils connectés.

> Gardez Intiface Central ouvert avec le serveur démarré pendant toute la session de jeu.

## Étape 2 — Installer l'addon dans WoW: Forever

1. Téléchargez ce dépôt (Code → Download ZIP) et décompressez-le.
2. Copiez le dossier `Butthurt` (celui qui contient `Butthurt.toc`) dans le dossier des addons du client **Forever** :
   ```
   C:\Program Files (x86)\World of Warcraft\_forever_\Interface\AddOns\Butthurt\
   ```
   Créez les dossiers `Interface\AddOns` s'ils n'existent pas. Attention : c'est bien le dossier `_forever_`, pas `_classic_` ni `_retail_`.
3. Lancez WoW: Forever. Au choix des personnages, cliquez sur **AddOns** (en bas à gauche) et vérifiez que **Butthurt** est coché. Le TOC déclare l'interface `16001`, celle du client 1.60.1 : pas besoin d'activer « Charger les addons obsolètes ».

L'addon dessine un pixel noir de 5×5 px dans le coin supérieur gauche de l'écran. C'est normal, c'est lui que lit le pont.

## Étape 3 — Installer le pont Butthurt.exe

1. Récupérez `Butthurt.exe` :
   - soit depuis la [release v0.2.0 du dépôt d'origine](https://github.com/Maikatura/Butthurt/releases) (dossier `Build/`),
   - soit en le compilant vous-même depuis [`Maikatura/Butthurt`](https://github.com/Maikatura/Butthurt) (solution `Butthurt.sln`, `dotnet build`). C'est l'option recommandée : lisez toujours le code d'un exécutable avant de le lancer.
2. Si Windows vous dit que le .NET Runtime manque, installez-le depuis <https://dotnet.microsoft.com/download>.

## Étape 4 — Jouer

1. Démarrez le serveur dans Intiface Central, jouet connecté.
2. Lancez WoW: Forever et connectez-vous à un personnage.
3. Lancez `Butthurt.exe` (double-clic, ou depuis un terminal pour voir ses messages : il affiche `Haptic device found.` si le jouet est bien détecté).
4. **Gardez la fenêtre de WoW au premier plan** : le pont lit le pixel du coin supérieur gauche de la fenêtre active. Une autre fenêtre par-dessus = plus de détection.
5. Prenez des dégâts → le pixel clignote en rouge → le jouet vibre. Appuyez sur **Échap** dans le terminal de Butthurt.exe pour arrêter.

## Différences avec la version d'origine (retail)

- **Le combat log n'existe pas sur le client Forever** : `COMBAT_LOG_EVENT_UNFILTERED` et `CombatLogGetCurrentEventInfo()` ne sont pas disponibles pour les addons. La détection de dégâts utilise donc une baisse de vie via `UNIT_HEALTH`.
- **Valeurs secrètes** : sur Forever, `UnitHealth` peut renvoyer une valeur « secrète » (règles Midnight) qu'il est interdit de comparer. L'addon détecte ce cas (`issecretvalue`) et bascule alors en mode dégradé : le pixel clignote à chaque changement de vie en combat (soins inclus). Si une future mise à jour du client retire les valeurs secrètes, le mode précis se réactive tout seul.
- **Pas de bibliothèque Ace3** : l'addon est autonome, ce qui évite les problèmes connus d'AceDB sur ce client (bug `regionKey`).

## Dépannage

- **Rien ne se passe quand je prends des dégâts** : vérifiez que la fenêtre de WoW est au premier plan, que le pixel est bien visible en haut à gauche (pas masqué par un autre élément d'UI), et que Butthurt.exe affiche `Haptic device found.`
- **`No haptic devices found`** : Intiface Central n'est pas démarré / le serveur n'écoute pas sur le port 12345 / le jouet n'est pas appairé. Reprenez l'étape 1.
- **L'addon n'apparaît pas en jeu** : mauvais dossier (`_forever_` et pas un autre), ou le dossier ne s'appelle pas `Butthurt` avec le `Butthurt.toc` directement dedans.

## Crédits

- Addon d'origine et pont Windows : [Maikatura/Butthurt](https://github.com/Maikatura/Butthurt)
- [Intiface Central](https://intiface.com/central/) / [Buttplug.io](https://buttplug.io)

Projet de fan, sans lien avec Blizzard Entertainment. Utilisez l'addon conformément aux règles du jeu.
