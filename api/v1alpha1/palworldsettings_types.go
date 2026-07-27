/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code in this file is generated from the researched Palworld 1.0
// DefaultPalWorldSettings.ini key list. The `pal:"<IniKey>,<kind>,<quote>"`
// struct tag is the single source of truth consumed by the INI renderer in
// internal/settings. Keys managed by the operator (passwords, public
// address, RCON/REST enablement and ports) are intentionally excluded here and
// injected at render time.
//
// DO NOT EDIT field tags by hand without updating the renderer accordingly.

package v1alpha1

// Difficulty is a Palworld enum setting.
// +kubebuilder:validation:Enum=None;Casual;Normal;Hard;Difficult
type Difficulty string

const (
	DifficultyNone      Difficulty = "None"
	DifficultyCasual    Difficulty = "Casual"
	DifficultyNormal    Difficulty = "Normal"
	DifficultyHard      Difficulty = "Hard"
	DifficultyDifficult Difficulty = "Difficult"
)

// RandomizerType is a Palworld enum setting.
// +kubebuilder:validation:Enum=None;Region;All
type RandomizerType string

const (
	RandomizerTypeNone   RandomizerType = "None"
	RandomizerTypeRegion RandomizerType = "Region"
	RandomizerTypeAll    RandomizerType = "All"
)

// DeathPenalty is a Palworld enum setting.
// +kubebuilder:validation:Enum=None;Item;ItemAndEquipment;All
type DeathPenalty string

const (
	DeathPenaltyNone             DeathPenalty = "None"
	DeathPenaltyItem             DeathPenalty = "Item"
	DeathPenaltyItemAndEquipment DeathPenalty = "ItemAndEquipment"
	DeathPenaltyAll              DeathPenalty = "All"
)

// LogFormatType is a Palworld enum setting.
// +kubebuilder:validation:Enum=Text;Json
type LogFormatType string

const (
	LogFormatTypeText LogFormatType = "Text"
	LogFormatTypeJson LogFormatType = "Json"
)

// CrossplayPlatform is a platform that may join via crossplay.
// +kubebuilder:validation:Enum=Steam;Xbox;PS5;Mac
type CrossplayPlatform string

const (
	CrossplayPlatformSteam CrossplayPlatform = "Steam"
	CrossplayPlatformXbox  CrossplayPlatform = "Xbox"
	CrossplayPlatformPS5   CrossplayPlatform = "PS5"
	CrossplayPlatformMac   CrossplayPlatform = "Mac"
)

// PalworldServerSettings holds the full set of Palworld dedicated server
// options that are rendered into PalWorldSettings.ini. Every field maps 1:1 to
// an OptionSettings key. Fields left at their defaults reproduce the official
// shipped defaults.
type PalworldServerSettings struct {
	// Difficulty maps to the `Difficulty` server setting (default None).
	// World difficulty preset. None means custom/individual settings are used.
	// +kubebuilder:default=None
	// +optional
	Difficulty Difficulty `json:"difficulty,omitempty" pal:"Difficulty,enum,n"`

	// RandomizerType maps to the `RandomizerType` server setting (default None).
	// Pal spawn randomization mode. None=off, Region=randomize per region, All=fully randomized.
	// +kubebuilder:default=None
	// +optional
	RandomizerType RandomizerType `json:"randomizerType,omitempty" pal:"RandomizerType,enum,n"`

	// RandomizerSeed maps to the `RandomizerSeed` server setting (default "").
	// Seed value used when Pal spawn randomization is enabled. Empty by default.
	// +optional
	RandomizerSeed string `json:"randomizerSeed,omitempty" pal:"RandomizerSeed,string,q"`

	// IsRandomizerPalLevelRandom maps to the `bIsRandomizerPalLevelRandom` server setting (default False).
	// If true, wild Pal levels are fully randomized; if false, randomized within area-optimized range.
	// +kubebuilder:default=false
	// +optional
	IsRandomizerPalLevelRandom bool `json:"isRandomizerPalLevelRandom,omitempty" pal:"bIsRandomizerPalLevelRandom,bool,n"`

	// DayTimeSpeedRate maps to the `DayTimeSpeedRate` server setting (default 1.000000).
	// Day length multiplier. Lower = longer days.
	// +kubebuilder:default=1
	// +optional
	DayTimeSpeedRate float64 `json:"dayTimeSpeedRate,omitempty" pal:"DayTimeSpeedRate,float,n"`

	// NightTimeSpeedRate maps to the `NightTimeSpeedRate` server setting (default 1.000000).
	// Night length multiplier. Lower = longer nights.
	// +kubebuilder:default=1
	// +optional
	NightTimeSpeedRate float64 `json:"nightTimeSpeedRate,omitempty" pal:"NightTimeSpeedRate,float,n"`

	// ExpRate maps to the `ExpRate` server setting (default 1.000000).
	// EXP gain multiplier.
	// +kubebuilder:default=1
	// +optional
	ExpRate float64 `json:"expRate,omitempty" pal:"ExpRate,float,n"`

	// PalCaptureRate maps to the `PalCaptureRate` server setting (default 1.000000).
	// Pal capture chance multiplier.
	// +kubebuilder:default=1
	// +optional
	PalCaptureRate float64 `json:"palCaptureRate,omitempty" pal:"PalCaptureRate,float,n"`

	// PalSpawnNumRate maps to the `PalSpawnNumRate` server setting (default 1.000000).
	// Wild Pal spawn quantity multiplier.
	// +kubebuilder:default=1
	// +optional
	PalSpawnNumRate float64 `json:"palSpawnNumRate,omitempty" pal:"PalSpawnNumRate,float,n"`

	// PalDamageRateAttack maps to the `PalDamageRateAttack` server setting (default 1.000000).
	// Damage dealt by Pals multiplier.
	// +kubebuilder:default=1
	// +optional
	PalDamageRateAttack float64 `json:"palDamageRateAttack,omitempty" pal:"PalDamageRateAttack,float,n"`

	// PalDamageRateDefense maps to the `PalDamageRateDefense` server setting (default 1.000000).
	// Damage taken by Pals multiplier.
	// +kubebuilder:default=1
	// +optional
	PalDamageRateDefense float64 `json:"palDamageRateDefense,omitempty" pal:"PalDamageRateDefense,float,n"`

	// PlayerDamageRateAttack maps to the `PlayerDamageRateAttack` server setting (default 1.000000).
	// Damage dealt by players multiplier.
	// +kubebuilder:default=1
	// +optional
	PlayerDamageRateAttack float64 `json:"playerDamageRateAttack,omitempty" pal:"PlayerDamageRateAttack,float,n"`

	// PlayerDamageRateDefense maps to the `PlayerDamageRateDefense` server setting (default 1.000000).
	// Damage taken by players multiplier.
	// +kubebuilder:default=1
	// +optional
	PlayerDamageRateDefense float64 `json:"playerDamageRateDefense,omitempty" pal:"PlayerDamageRateDefense,float,n"`

	// PlayerStomachDecreaceRate maps to the `PlayerStomachDecreaceRate` server setting (default 1.000000).
	// Player hunger depletion rate. Note: misspelling 'Decreace' is intentional/canonical.
	// +kubebuilder:default=1
	// +optional
	PlayerStomachDecreaceRate float64 `json:"playerStomachDecreaceRate,omitempty" pal:"PlayerStomachDecreaceRate,float,n"`

	// PlayerStaminaDecreaceRate maps to the `PlayerStaminaDecreaceRate` server setting (default 1.000000).
	// Player stamina depletion rate. 'Decreace' spelling is canonical.
	// +kubebuilder:default=1
	// +optional
	PlayerStaminaDecreaceRate float64 `json:"playerStaminaDecreaceRate,omitempty" pal:"PlayerStaminaDecreaceRate,float,n"`

	// PlayerAutoHPRegeneRate maps to the `PlayerAutoHPRegeneRate` server setting (default 1.000000).
	// Player passive HP regen rate. Note capitalization HP.
	// +kubebuilder:default=1
	// +optional
	PlayerAutoHPRegeneRate float64 `json:"playerAutoHPRegeneRate,omitempty" pal:"PlayerAutoHPRegeneRate,float,n"`

	// PlayerAutoHpRegeneRateInSleep maps to the `PlayerAutoHpRegeneRateInSleep` server setting (default 1.000000).
	// Player HP regen rate while sleeping. Note capitalization Hp (lowercase p).
	// +kubebuilder:default=1
	// +optional
	PlayerAutoHpRegeneRateInSleep float64 `json:"playerAutoHpRegeneRateInSleep,omitempty" pal:"PlayerAutoHpRegeneRateInSleep,float,n"`

	// PalStomachDecreaceRate maps to the `PalStomachDecreaceRate` server setting (default 1.000000).
	// Pal hunger depletion rate.
	// +kubebuilder:default=1
	// +optional
	PalStomachDecreaceRate float64 `json:"palStomachDecreaceRate,omitempty" pal:"PalStomachDecreaceRate,float,n"`

	// PalStaminaDecreaceRate maps to the `PalStaminaDecreaceRate` server setting (default 1.000000).
	// Pal stamina depletion rate.
	// +kubebuilder:default=1
	// +optional
	PalStaminaDecreaceRate float64 `json:"palStaminaDecreaceRate,omitempty" pal:"PalStaminaDecreaceRate,float,n"`

	// PalAutoHPRegeneRate maps to the `PalAutoHPRegeneRate` server setting (default 1.000000).
	// Pal passive HP regen rate (HP uppercase).
	// +kubebuilder:default=1
	// +optional
	PalAutoHPRegeneRate float64 `json:"palAutoHPRegeneRate,omitempty" pal:"PalAutoHPRegeneRate,float,n"`

	// PalAutoHpRegeneRateInSleep maps to the `PalAutoHpRegeneRateInSleep` server setting (default 1.000000).
	// Pal HP regen rate in Palbox/sleep (Hp lowercase p).
	// +kubebuilder:default=1
	// +optional
	PalAutoHpRegeneRateInSleep float64 `json:"palAutoHpRegeneRateInSleep,omitempty" pal:"PalAutoHpRegeneRateInSleep,float,n"`

	// BuildObjectHpRate maps to the `BuildObjectHpRate` server setting (default 1.000000).
	// Structure HP multiplier. Added post-launch; not present in pre-1.0 default ini.
	// +kubebuilder:default=1
	// +optional
	BuildObjectHpRate float64 `json:"buildObjectHpRate,omitempty" pal:"BuildObjectHpRate,float,n"`

	// BuildObjectDamageRate maps to the `BuildObjectDamageRate` server setting (default 1.000000).
	// Damage to structures multiplier.
	// +kubebuilder:default=1
	// +optional
	BuildObjectDamageRate float64 `json:"buildObjectDamageRate,omitempty" pal:"BuildObjectDamageRate,float,n"`

	// BuildObjectDeteriorationDamageRate maps to the `BuildObjectDeteriorationDamageRate` server setting (default 1.000000).
	// Structure deterioration rate. 0 = no deterioration.
	// +kubebuilder:default=1
	// +optional
	BuildObjectDeteriorationDamageRate float64 `json:"buildObjectDeteriorationDamageRate,omitempty" pal:"BuildObjectDeteriorationDamageRate,float,n"`

	// CollectionDropRate maps to the `CollectionDropRate` server setting (default 1.000000).
	// Gatherable item yield multiplier.
	// +kubebuilder:default=1
	// +optional
	CollectionDropRate float64 `json:"collectionDropRate,omitempty" pal:"CollectionDropRate,float,n"`

	// CollectionObjectHpRate maps to the `CollectionObjectHpRate` server setting (default 1.000000).
	// Gatherable object HP multiplier.
	// +kubebuilder:default=1
	// +optional
	CollectionObjectHpRate float64 `json:"collectionObjectHpRate,omitempty" pal:"CollectionObjectHpRate,float,n"`

	// CollectionObjectRespawnSpeedRate maps to the `CollectionObjectRespawnSpeedRate` server setting (default 1.000000).
	// Gatherable object respawn interval multiplier.
	// +kubebuilder:default=1
	// +optional
	CollectionObjectRespawnSpeedRate float64 `json:"collectionObjectRespawnSpeedRate,omitempty" pal:"CollectionObjectRespawnSpeedRate,float,n"`

	// EnemyDropItemRate maps to the `EnemyDropItemRate` server setting (default 1.000000).
	// Dropped item quantity multiplier from defeating Pals.
	// +kubebuilder:default=1
	// +optional
	EnemyDropItemRate float64 `json:"enemyDropItemRate,omitempty" pal:"EnemyDropItemRate,float,n"`

	// DeathPenalty maps to the `DeathPenalty` server setting (default All).
	// On death: None=lose nothing, Item=lose inventory items only, ItemAndEquipment=lose items+equipment, All=lose items+equipment+Pals in inventory. OFFICIAL shipped default is All; some host presets use Item.
	// +kubebuilder:default=All
	// +optional
	DeathPenalty DeathPenalty `json:"deathPenalty,omitempty" pal:"DeathPenalty,enum,n"`

	// EnablePlayerToPlayerDamage maps to the `bEnablePlayerToPlayerDamage` server setting (default False).
	// Allow players to damage other players.
	// +kubebuilder:default=false
	// +optional
	EnablePlayerToPlayerDamage bool `json:"enablePlayerToPlayerDamage,omitempty" pal:"bEnablePlayerToPlayerDamage,bool,n"`

	// EnableFriendlyFire maps to the `bEnableFriendlyFire` server setting (default False).
	// Allow friendly fire within a guild/party.
	// +kubebuilder:default=false
	// +optional
	EnableFriendlyFire bool `json:"enableFriendlyFire,omitempty" pal:"bEnableFriendlyFire,bool,n"`

	// EnableInvaderEnemy maps to the `bEnableInvaderEnemy` server setting (default True).
	// Enable raid events on bases.
	// +kubebuilder:default=true
	// +optional
	EnableInvaderEnemy bool `json:"enableInvaderEnemy,omitempty" pal:"bEnableInvaderEnemy,bool,n"`

	// ActiveUNKO maps to the `bActiveUNKO` server setting (default False).
	// Enable UNKO (poop) mechanic.
	// +kubebuilder:default=false
	// +optional
	ActiveUNKO bool `json:"activeUNKO,omitempty" pal:"bActiveUNKO,bool,n"`

	// EnableAimAssistPad maps to the `bEnableAimAssistPad` server setting (default True).
	// Enable controller aim assist.
	// +kubebuilder:default=true
	// +optional
	EnableAimAssistPad bool `json:"enableAimAssistPad,omitempty" pal:"bEnableAimAssistPad,bool,n"`

	// EnableAimAssistKeyboard maps to the `bEnableAimAssistKeyboard` server setting (default False).
	// Enable keyboard aim assist.
	// +kubebuilder:default=false
	// +optional
	EnableAimAssistKeyboard bool `json:"enableAimAssistKeyboard,omitempty" pal:"bEnableAimAssistKeyboard,bool,n"`

	// DropItemMaxNum maps to the `DropItemMaxNum` server setting (default 3000).
	// Max number of dropped items in the world.
	// +kubebuilder:default=3000
	// +optional
	DropItemMaxNum int32 `json:"dropItemMaxNum,omitempty" pal:"DropItemMaxNum,int,n"`

	// PhysicsActiveDropItemMaxNum maps to the `PhysicsActiveDropItemMaxNum` server setting (default -1).
	// Max dropped items using physics behavior. -1 = unlimited. Post-launch key.
	// +kubebuilder:default=-1
	// +optional
	PhysicsActiveDropItemMaxNum int32 `json:"physicsActiveDropItemMaxNum,omitempty" pal:"PhysicsActiveDropItemMaxNum,int,n"`

	// DropItemMaxNumUNKO maps to the `DropItemMaxNum_UNKO` server setting (default 100).
	// Max UNKO drops in the world. Note underscore in key name.
	// +kubebuilder:default=100
	// +optional
	DropItemMaxNumUNKO int32 `json:"dropItemMaxNumUNKO,omitempty" pal:"DropItemMaxNum_UNKO,int,n"`

	// BaseCampMaxNum maps to the `BaseCampMaxNum` server setting (default 128).
	// Max number of base camps in the world.
	// +kubebuilder:default=128
	// +optional
	BaseCampMaxNum int32 `json:"baseCampMaxNum,omitempty" pal:"BaseCampMaxNum,int,n"`

	// BaseCampWorkerMaxNum maps to the `BaseCampWorkerMaxNum` server setting (default 15).
	// Max working Pals per base camp. Known to be ignored by some server builds (game bug).
	// +kubebuilder:default=15
	// +optional
	BaseCampWorkerMaxNum int32 `json:"baseCampWorkerMaxNum,omitempty" pal:"BaseCampWorkerMaxNum,int,n"`

	// DropItemAliveMaxHours maps to the `DropItemAliveMaxHours` server setting (default 1.000000).
	// Hours before dropped items despawn.
	// +kubebuilder:default=1
	// +optional
	DropItemAliveMaxHours float64 `json:"dropItemAliveMaxHours,omitempty" pal:"DropItemAliveMaxHours,float,n"`

	// AutoResetGuildNoOnlinePlayers maps to the `bAutoResetGuildNoOnlinePlayers` server setting (default False).
	// Auto-reset a guild when it has no online players.
	// +kubebuilder:default=false
	// +optional
	AutoResetGuildNoOnlinePlayers bool `json:"autoResetGuildNoOnlinePlayers,omitempty" pal:"bAutoResetGuildNoOnlinePlayers,bool,n"`

	// AutoResetGuildTimeNoOnlinePlayers maps to the `AutoResetGuildTimeNoOnlinePlayers` server setting (default 72.000000).
	// Hours of no online players before a guild auto-resets.
	// +kubebuilder:default=72
	// +optional
	AutoResetGuildTimeNoOnlinePlayers float64 `json:"autoResetGuildTimeNoOnlinePlayers,omitempty" pal:"AutoResetGuildTimeNoOnlinePlayers,float,n"`

	// GuildPlayerMaxNum maps to the `GuildPlayerMaxNum` server setting (default 20).
	// Max players per guild.
	// +kubebuilder:default=20
	// +optional
	GuildPlayerMaxNum int32 `json:"guildPlayerMaxNum,omitempty" pal:"GuildPlayerMaxNum,int,n"`

	// BaseCampMaxNumInGuild maps to the `BaseCampMaxNumInGuild` server setting (default 4).
	// Max base camps per guild (max 10). Post-launch key.
	// +kubebuilder:default=4
	// +optional
	BaseCampMaxNumInGuild int32 `json:"baseCampMaxNumInGuild,omitempty" pal:"BaseCampMaxNumInGuild,int,n"`

	// PalEggDefaultHatchingTime maps to the `PalEggDefaultHatchingTime` server setting (default 72.000000).
	// Hours to hatch a Huge Egg. OFFICIAL shipped default is 72; many host presets set 1 for faster hatching.
	// +kubebuilder:default=72
	// +optional
	PalEggDefaultHatchingTime float64 `json:"palEggDefaultHatchingTime,omitempty" pal:"PalEggDefaultHatchingTime,float,n"`

	// WorkSpeedRate maps to the `WorkSpeedRate` server setting (default 1.000000).
	// Base work speed multiplier.
	// +kubebuilder:default=1
	// +optional
	WorkSpeedRate float64 `json:"workSpeedRate,omitempty" pal:"WorkSpeedRate,float,n"`

	// AutoSaveSpan maps to the `AutoSaveSpan` server setting (default 30.000000).
	// Auto-save interval (minutes). Post-launch key.
	// +kubebuilder:default=30
	// +optional
	AutoSaveSpan float64 `json:"autoSaveSpan,omitempty" pal:"AutoSaveSpan,float,n"`

	// IsMultiplay maps to the `bIsMultiplay` server setting (default False).
	// Enable co-op multiplayer (relevant for invite sessions, not dedicated servers).
	// +kubebuilder:default=false
	// +optional
	IsMultiplay bool `json:"isMultiplay,omitempty" pal:"bIsMultiplay,bool,n"`

	// IsPvP maps to the `bIsPvP` server setting (default False).
	// Enable PvP.
	// +kubebuilder:default=false
	// +optional
	IsPvP bool `json:"isPvP,omitempty" pal:"bIsPvP,bool,n"`

	// Hardcore maps to the `bHardcore` server setting (default False).
	// Enable Hardcore mode (permadeath). Post-launch key.
	// +kubebuilder:default=false
	// +optional
	Hardcore bool `json:"hardcore,omitempty" pal:"bHardcore,bool,n"`

	// PalLost maps to the `bPalLost` server setting (default False).
	// Permanently lose Pals on death. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	PalLost bool `json:"palLost,omitempty" pal:"bPalLost,bool,n"`

	// CharacterRecreateInHardcore maps to the `bCharacterRecreateInHardcore` server setting (default False).
	// Allow recreating character after death in Hardcore. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	CharacterRecreateInHardcore bool `json:"characterRecreateInHardcore,omitempty" pal:"bCharacterRecreateInHardcore,bool,n"`

	// CanPickupOtherGuildDeathPenaltyDrop maps to the `bCanPickupOtherGuildDeathPenaltyDrop` server setting (default False).
	// Allow other guilds to pick up your death-penalty drops.
	// +kubebuilder:default=false
	// +optional
	CanPickupOtherGuildDeathPenaltyDrop bool `json:"canPickupOtherGuildDeathPenaltyDrop,omitempty" pal:"bCanPickupOtherGuildDeathPenaltyDrop,bool,n"`

	// EnableNonLoginPenalty maps to the `bEnableNonLoginPenalty` server setting (default True).
	// Enable penalty for not logging in.
	// +kubebuilder:default=true
	// +optional
	EnableNonLoginPenalty bool `json:"enableNonLoginPenalty,omitempty" pal:"bEnableNonLoginPenalty,bool,n"`

	// EnableFastTravel maps to the `bEnableFastTravel` server setting (default True).
	// Enable fast travel.
	// +kubebuilder:default=true
	// +optional
	EnableFastTravel bool `json:"enableFastTravel,omitempty" pal:"bEnableFastTravel,bool,n"`

	// EnableFastTravelOnlyBaseCamp maps to the `bEnableFastTravelOnlyBaseCamp` server setting (default False).
	// Restrict fast travel to base camps only. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	EnableFastTravelOnlyBaseCamp bool `json:"enableFastTravelOnlyBaseCamp,omitempty" pal:"bEnableFastTravelOnlyBaseCamp,bool,n"`

	// IsStartLocationSelectByMap maps to the `bIsStartLocationSelectByMap` server setting (default True).
	// Allow selecting start location on the map. OFFICIAL shipped default is True; some host presets set False.
	// +kubebuilder:default=true
	// +optional
	IsStartLocationSelectByMap bool `json:"isStartLocationSelectByMap,omitempty" pal:"bIsStartLocationSelectByMap,bool,n"`

	// ExistPlayerAfterLogout maps to the `bExistPlayerAfterLogout` server setting (default False).
	// Keep player body (sleeping) in world after logout.
	// +kubebuilder:default=false
	// +optional
	ExistPlayerAfterLogout bool `json:"existPlayerAfterLogout,omitempty" pal:"bExistPlayerAfterLogout,bool,n"`

	// EnableDefenseOtherGuildPlayer maps to the `bEnableDefenseOtherGuildPlayer` server setting (default False).
	// Allow base defense against other guild players.
	// +kubebuilder:default=false
	// +optional
	EnableDefenseOtherGuildPlayer bool `json:"enableDefenseOtherGuildPlayer,omitempty" pal:"bEnableDefenseOtherGuildPlayer,bool,n"`

	// InvisibleOtherGuildBaseCampAreaFX maps to the `bInvisibleOtherGuildBaseCampAreaFX` server setting (default False).
	// Hide other guilds' base-camp area effects. Note canonical key spelling 'bInvisible' (the jammsen ENV var is misspelled INVISBIBLE). Post-launch key.
	// +kubebuilder:default=false
	// +optional
	InvisibleOtherGuildBaseCampAreaFX bool `json:"invisibleOtherGuildBaseCampAreaFX,omitempty" pal:"bInvisibleOtherGuildBaseCampAreaFX,bool,n"`

	// BuildAreaLimit maps to the `bBuildAreaLimit` server setting (default False).
	// Prohibit building near structures like fast-travel points. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	BuildAreaLimit bool `json:"buildAreaLimit,omitempty" pal:"bBuildAreaLimit,bool,n"`

	// ItemWeightRate maps to the `ItemWeightRate` server setting (default 1.000000).
	// Item weight multiplier. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	ItemWeightRate float64 `json:"itemWeightRate,omitempty" pal:"ItemWeightRate,float,n"`

	// CoopPlayerMaxNum maps to the `CoopPlayerMaxNum` server setting (default 4).
	// Max players in a co-op (invite-code) session; not used by dedicated servers.
	// +kubebuilder:default=4
	// +optional
	CoopPlayerMaxNum int32 `json:"coopPlayerMaxNum,omitempty" pal:"CoopPlayerMaxNum,int,n"`

	// ServerPlayerMaxNum maps to the `ServerPlayerMaxNum` server setting (default 32).
	// Max players on the dedicated server. This is the real dedicated-server player cap (not CoopPlayerMaxNum).
	// +kubebuilder:default=32
	// +optional
	ServerPlayerMaxNum int32 `json:"serverPlayerMaxNum,omitempty" pal:"ServerPlayerMaxNum,int,n"`

	// ServerName maps to the `ServerName` server setting (default "Default Palworld Server").
	// Server display name. OFFICIAL shipped default is "Default Palworld Server".
	// +kubebuilder:default="Default Palworld Server"
	// +optional
	ServerName string `json:"serverName,omitempty" pal:"ServerName,string,q"`

	// ServerDescription maps to the `ServerDescription` server setting (default "").
	// Server description. Empty by default.
	// +optional
	ServerDescription string `json:"serverDescription,omitempty" pal:"ServerDescription,string,q"`

	// AllowClientMod maps to the `bAllowClientMod` server setting (default True).
	// Allow clients with mods to join. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowClientMod bool `json:"allowClientMod,omitempty" pal:"bAllowClientMod,bool,n"`

	// Region maps to the `Region` server setting (default "").
	// Server region tag. Empty by default.
	// +optional
	Region string `json:"region,omitempty" pal:"Region,string,q"`

	// UseAuth maps to the `bUseAuth` server setting (default True).
	// Require platform authentication to connect.
	// +kubebuilder:default=true
	// +optional
	UseAuth bool `json:"useAuth,omitempty" pal:"bUseAuth,bool,n"`

	// BanListURL maps to the `BanListURL` server setting (default "https://api.palworldgame.com/api/banlist.txt").
	// URL of the ban list. OFFICIAL shipped default is https://api.palworldgame.com/api/banlist.txt; note this endpoint returns an empty list since 1.0, so some hosts switch to https://b.palworldgame.com/api/banlist.txt.
	// +kubebuilder:default="https://api.palworldgame.com/api/banlist.txt"
	// +optional
	BanListURL string `json:"banListURL,omitempty" pal:"BanListURL,string,q"`

	// ShowPlayerList maps to the `bShowPlayerList` server setting (default False).
	// Show in-game player list (Esc menu). Post-launch key. Some sources report the shipped default as True; verify against your build.
	// +kubebuilder:default=false
	// +optional
	ShowPlayerList bool `json:"showPlayerList,omitempty" pal:"bShowPlayerList,bool,n"`

	// ChatPostLimitPerMinute maps to the `ChatPostLimitPerMinute` server setting (default 30).
	// Max chat messages a player may post per minute. Post-launch key.
	// +kubebuilder:default=30
	// +optional
	ChatPostLimitPerMinute int32 `json:"chatPostLimitPerMinute,omitempty" pal:"ChatPostLimitPerMinute,int,n"`

	// CrossplayPlatforms maps to the `CrossplayPlatforms` server setting (default (Steam,Xbox,PS5,Mac)).
	// Allowed connecting platforms. Emitted as a bare parenthesized comma-separated tuple, e.g. (Steam,Xbox,PS5,Mac) — NOT wrapped in double quotes. Replaces the deprecated AllowConnectPlatform. Restrict by listing a subset, e.g. (Steam).
	// +kubebuilder:default={Steam,Xbox,PS5,Mac}
	// +optional
	CrossplayPlatforms []CrossplayPlatform `json:"crossplayPlatforms,omitempty" pal:"CrossplayPlatforms,platforms,n"`

	// IsUseBackupSaveData maps to the `bIsUseBackupSaveData` server setting (default True).
	// Enable the game server's internal world save backups. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	IsUseBackupSaveData bool `json:"isUseBackupSaveData,omitempty" pal:"bIsUseBackupSaveData,bool,n"`

	// LogFormatType maps to the `LogFormatType` server setting (default Text).
	// Server log output format. Unquoted enum like Difficulty/DeathPenalty.
	// +kubebuilder:default=Text
	// +optional
	LogFormatType LogFormatType `json:"logFormatType,omitempty" pal:"LogFormatType,enum,n"`

	// IsShowJoinLeftMessage maps to the `bIsShowJoinLeftMessage` server setting (default True).
	// Show join/leave messages in chat. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	IsShowJoinLeftMessage bool `json:"isShowJoinLeftMessage,omitempty" pal:"bIsShowJoinLeftMessage,bool,n"`

	// SupplyDropSpan maps to the `SupplyDropSpan` server setting (default 180).
	// Interval (minutes) between supply/meteorite drops. Post-launch key.
	// +kubebuilder:default=180
	// +optional
	SupplyDropSpan int32 `json:"supplyDropSpan,omitempty" pal:"SupplyDropSpan,int,n"`

	// EnablePredatorBossPal maps to the `EnablePredatorBossPal` server setting (default True).
	// Enable Predator (boss) Pals. Boolean value but note the key has NO leading 'b' prefix. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	EnablePredatorBossPal bool `json:"enablePredatorBossPal,omitempty" pal:"EnablePredatorBossPal,bool,n"`

	// MaxBuildingLimitNum maps to the `MaxBuildingLimitNum` server setting (default 0).
	// Per-player building limit. 0 = unlimited. Post-launch key.
	// +kubebuilder:default=0
	// +optional
	MaxBuildingLimitNum int32 `json:"maxBuildingLimitNum,omitempty" pal:"MaxBuildingLimitNum,int,n"`

	// ServerReplicatePawnCullDistance maps to the `ServerReplicatePawnCullDistance` server setting (default 15000.000000).
	// Pal replication/sync distance from player (cm). Min 5000, max 15000. Post-launch key.
	// +kubebuilder:default=15000
	// +optional
	ServerReplicatePawnCullDistance float64 `json:"serverReplicatePawnCullDistance,omitempty" pal:"ServerReplicatePawnCullDistance,float,n"`

	// AllowGlobalPalboxExport maps to the `bAllowGlobalPalboxExport` server setting (default True).
	// Allow saving/exporting to the global Palbox. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowGlobalPalboxExport bool `json:"allowGlobalPalboxExport,omitempty" pal:"bAllowGlobalPalboxExport,bool,n"`

	// AllowGlobalPalboxImport maps to the `bAllowGlobalPalboxImport` server setting (default False).
	// Allow importing from the global Palbox. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	AllowGlobalPalboxImport bool `json:"allowGlobalPalboxImport,omitempty" pal:"bAllowGlobalPalboxImport,bool,n"`

	// EquipmentDurabilityDamageRate maps to the `EquipmentDurabilityDamageRate` server setting (default 1.000000).
	// Equipment durability loss multiplier. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	EquipmentDurabilityDamageRate float64 `json:"equipmentDurabilityDamageRate,omitempty" pal:"EquipmentDurabilityDamageRate,float,n"`

	// ItemContainerForceMarkDirtyInterval maps to the `ItemContainerForceMarkDirtyInterval` server setting (default 1.000000).
	// Force-sync interval (seconds) when opening a container. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	ItemContainerForceMarkDirtyInterval float64 `json:"itemContainerForceMarkDirtyInterval,omitempty" pal:"ItemContainerForceMarkDirtyInterval,float,n"`

	// PlayerDataPalStorageUpdateCheckTickInterval maps to the `PlayerDataPalStorageUpdateCheckTickInterval` server setting (default 1.000000).
	// Interval (seconds) for checking Pal-storage updates in player data. Post-launch key; sparsely documented.
	// +kubebuilder:default=1
	// +optional
	PlayerDataPalStorageUpdateCheckTickInterval float64 `json:"playerDataPalStorageUpdateCheckTickInterval,omitempty" pal:"PlayerDataPalStorageUpdateCheckTickInterval,float,n"`

	// ItemCorruptionMultiplier maps to the `ItemCorruptionMultiplier` server setting (default 1.000000).
	// Item corruption/decay speed multiplier. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	ItemCorruptionMultiplier float64 `json:"itemCorruptionMultiplier,omitempty" pal:"ItemCorruptionMultiplier,float,n"`

	// MonsterFarmActionSpeedRate maps to the `MonsterFarmActionSpeedRate` server setting (default 1.000000).
	// Ranch (grazing) item production speed multiplier. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	MonsterFarmActionSpeedRate float64 `json:"monsterFarmActionSpeedRate,omitempty" pal:"MonsterFarmActionSpeedRate,float,n"`

	// DenyTechnologyList maps to the `DenyTechnologyList` server setting.
	// Comma-separated Technology IDs to disable. Empty by default. Emitted UNQUOTED in the template (i.e. DenyTechnologyList= with an empty value). Post-launch key.
	// +optional
	DenyTechnologyList string `json:"denyTechnologyList,omitempty" pal:"DenyTechnologyList,string,q"`

	// GuildRejoinCooldownMinutes maps to the `GuildRejoinCooldownMinutes` server setting (default 0).
	// Cooldown (minutes) before a player can rejoin a guild. Post-launch key.
	// +kubebuilder:default=0
	// +optional
	GuildRejoinCooldownMinutes int32 `json:"guildRejoinCooldownMinutes,omitempty" pal:"GuildRejoinCooldownMinutes,int,n"`

	// AutoTransferMasterCheckIntervalSeconds maps to the `AutoTransferMasterCheckIntervalSeconds` server setting (default 3600.000000).
	// Interval (seconds) between checks for auto-transfer of guild master. Post-launch key; sparsely documented.
	// +kubebuilder:default=3600
	// +optional
	AutoTransferMasterCheckIntervalSeconds float64 `json:"autoTransferMasterCheckIntervalSeconds,omitempty" pal:"AutoTransferMasterCheckIntervalSeconds,float,n"`

	// AutoTransferMasterThresholdDays maps to the `AutoTransferMasterThresholdDays` server setting (default 14).
	// Days of guild-master inactivity before the role auto-transfers. Post-launch key.
	// +kubebuilder:default=14
	// +optional
	AutoTransferMasterThresholdDays int32 `json:"autoTransferMasterThresholdDays,omitempty" pal:"AutoTransferMasterThresholdDays,int,n"`

	// MaxGuildsPerFrame maps to the `MaxGuildsPerFrame` server setting (default 10).
	// Number of guilds processed per server frame. Post-launch key; sparsely documented.
	// +kubebuilder:default=10
	// +optional
	MaxGuildsPerFrame int32 `json:"maxGuildsPerFrame,omitempty" pal:"MaxGuildsPerFrame,int,n"`

	// BlockRespawnTime maps to the `BlockRespawnTime` server setting (default 5.000000).
	// Cooldown (seconds) before respawn after death. Post-launch key.
	// +kubebuilder:default=5
	// +optional
	BlockRespawnTime float64 `json:"blockRespawnTime,omitempty" pal:"BlockRespawnTime,float,n"`

	// RespawnPenaltyDurationThreshold maps to the `RespawnPenaltyDurationThreshold` server setting (default 0.000000).
	// Survival-time threshold (seconds) for applying the respawn cooldown multiplier. Post-launch key.
	// +kubebuilder:default=0
	// +optional
	RespawnPenaltyDurationThreshold float64 `json:"respawnPenaltyDurationThreshold,omitempty" pal:"RespawnPenaltyDurationThreshold,float,n"`

	// RespawnPenaltyTimeScale maps to the `RespawnPenaltyTimeScale` server setting (default 2.000000).
	// Multiplier applied to the respawn cooldown. Post-launch key.
	// +kubebuilder:default=2
	// +optional
	RespawnPenaltyTimeScale float64 `json:"respawnPenaltyTimeScale,omitempty" pal:"RespawnPenaltyTimeScale,float,n"`

	// DisplayPvPItemNumOnWorldMapBaseCamp maps to the `bDisplayPvPItemNumOnWorldMap_BaseCamp` server setting (default False).
	// Show count of PvP-exclusive items per base on the world map. Note underscore in key. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	DisplayPvPItemNumOnWorldMapBaseCamp bool `json:"displayPvPItemNumOnWorldMapBaseCamp,omitempty" pal:"bDisplayPvPItemNumOnWorldMap_BaseCamp,bool,n"`

	// DisplayPvPItemNumOnWorldMapPlayer maps to the `bDisplayPvPItemNumOnWorldMap_Player` server setting (default False).
	// Show player locations and PvP-exclusive item counts on the world map. Note underscore in key. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	DisplayPvPItemNumOnWorldMapPlayer bool `json:"displayPvPItemNumOnWorldMapPlayer,omitempty" pal:"bDisplayPvPItemNumOnWorldMap_Player,bool,n"`

	// AdditionalDropItemWhenPlayerKillingInPvPMode maps to the `AdditionalDropItemWhenPlayerKillingInPvPMode` server setting (default "PlayerDropItem").
	// Item ID dropped when a player is killed in PvP. String value, IS wrapped in double quotes. Post-launch key.
	// +kubebuilder:default="PlayerDropItem"
	// +optional
	AdditionalDropItemWhenPlayerKillingInPvPMode string `json:"additionalDropItemWhenPlayerKillingInPvPMode,omitempty" pal:"AdditionalDropItemWhenPlayerKillingInPvPMode,string,q"`

	// AdditionalDropItemNumWhenPlayerKillingInPvPMode maps to the `AdditionalDropItemNumWhenPlayerKillingInPvPMode` server setting (default 1).
	// Quantity of the special item dropped on PvP kill. Post-launch key.
	// +kubebuilder:default=1
	// +optional
	AdditionalDropItemNumWhenPlayerKillingInPvPMode int32 `json:"additionalDropItemNumWhenPlayerKillingInPvPMode,omitempty" pal:"AdditionalDropItemNumWhenPlayerKillingInPvPMode,int,n"`

	// BAdditionalDropItemWhenPlayerKillingInPvPMode maps to the `bAdditionalDropItemWhenPlayerKillingInPvPMode` server setting (default False).
	// Whether to drop the special item on a PvP kill. Note: same base name as the two keys above but 'b'-prefixed boolean toggle. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	BAdditionalDropItemWhenPlayerKillingInPvPMode bool `json:"bAdditionalDropItemWhenPlayerKillingInPvPMode,omitempty" pal:"bAdditionalDropItemWhenPlayerKillingInPvPMode,bool,n"`

	// EnableVoiceChat maps to the `bEnableVoiceChat` server setting (default False).
	// Enable in-game proximity voice chat. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	EnableVoiceChat bool `json:"enableVoiceChat,omitempty" pal:"bEnableVoiceChat,bool,n"`

	// VoiceChatMaxVolumeDistance maps to the `VoiceChatMaxVolumeDistance` server setting (default 3000.000000).
	// Distance (cm) up to which voice chat is at full volume. Post-launch key.
	// +kubebuilder:default=3000
	// +optional
	VoiceChatMaxVolumeDistance float64 `json:"voiceChatMaxVolumeDistance,omitempty" pal:"VoiceChatMaxVolumeDistance,float,n"`

	// VoiceChatZeroVolumeDistance maps to the `VoiceChatZeroVolumeDistance` server setting (default 15000.000000).
	// Distance (cm) at which voice chat volume reaches zero. Post-launch key.
	// +kubebuilder:default=15000
	// +optional
	VoiceChatZeroVolumeDistance float64 `json:"voiceChatZeroVolumeDistance,omitempty" pal:"VoiceChatZeroVolumeDistance,float,n"`

	// AllowEnhanceStatHealth maps to the `bAllowEnhanceStat_Health` server setting (default True).
	// Allow allocating stat points to Health. Note underscore in key. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowEnhanceStatHealth bool `json:"allowEnhanceStatHealth,omitempty" pal:"bAllowEnhanceStat_Health,bool,n"`

	// AllowEnhanceStatAttack maps to the `bAllowEnhanceStat_Attack` server setting (default True).
	// Allow allocating stat points to Attack. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowEnhanceStatAttack bool `json:"allowEnhanceStatAttack,omitempty" pal:"bAllowEnhanceStat_Attack,bool,n"`

	// AllowEnhanceStatStamina maps to the `bAllowEnhanceStat_Stamina` server setting (default True).
	// Allow allocating stat points to Stamina. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowEnhanceStatStamina bool `json:"allowEnhanceStatStamina,omitempty" pal:"bAllowEnhanceStat_Stamina,bool,n"`

	// AllowEnhanceStatWeight maps to the `bAllowEnhanceStat_Weight` server setting (default True).
	// Allow allocating stat points to Weight. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowEnhanceStatWeight bool `json:"allowEnhanceStatWeight,omitempty" pal:"bAllowEnhanceStat_Weight,bool,n"`

	// AllowEnhanceStatWorkSpeed maps to the `bAllowEnhanceStat_WorkSpeed` server setting (default True).
	// Allow allocating stat points to Work Speed. Post-launch key.
	// +kubebuilder:default=true
	// +optional
	AllowEnhanceStatWorkSpeed bool `json:"allowEnhanceStatWorkSpeed,omitempty" pal:"bAllowEnhanceStat_WorkSpeed,bool,n"`

	// EnableBuildingPlayerUIdDisplay maps to the `bEnableBuildingPlayerUIdDisplay` server setting (default False).
	// Display the creator's player UID on structures. Note capitalization 'UId'. Post-launch key.
	// +kubebuilder:default=false
	// +optional
	EnableBuildingPlayerUIdDisplay bool `json:"enableBuildingPlayerUIdDisplay,omitempty" pal:"bEnableBuildingPlayerUIdDisplay,bool,n"`

	// BuildingNameDisplayCacheTTLSeconds maps to the `BuildingNameDisplayCacheTTLSeconds` server setting (default 60).
	// Cache lifetime (seconds) for building creator-name display. Post-launch key; sparsely documented.
	// +kubebuilder:default=60
	// +optional
	BuildingNameDisplayCacheTTLSeconds int32 `json:"buildingNameDisplayCacheTTLSeconds,omitempty" pal:"BuildingNameDisplayCacheTTLSeconds,int,n"`
}
