-- Butthurt - WoW: Forever port
-- The combat log (CombatLogGetCurrentEventInfo / COMBAT_LOG_EVENT_UNFILTERED)
-- is not available to addons on the Forever client, so damage is detected
-- through UNIT_HEALTH drops instead.

local FLASH_DURATION = 0.2

local frame = CreateFrame("Frame")
local pixelFlashFrame = CreateFrame("Frame", "PixelFlashFrame", UIParent)
pixelFlashFrame:SetFrameStrata("HIGH")
pixelFlashFrame:SetWidth(5)
pixelFlashFrame:SetHeight(5)
pixelFlashFrame:SetPoint("TOPLEFT", UIParent, "TOPLEFT")
pixelFlashFrame.texture = pixelFlashFrame:CreateTexture(nil, "BACKGROUND")
pixelFlashFrame.texture:SetAllPoints(pixelFlashFrame)
pixelFlashFrame.texture:SetColorTexture(0, 0, 0, 1)

local lastHealth
local secretMode = false

local function Sealed(value)
    return type(issecretvalue) == "function" and issecretvalue(value)
end

local function Flash()
    pixelFlashFrame.texture:SetColorTexture(1, 0, 0, 1)
    C_Timer.After(FLASH_DURATION, function()
        pixelFlashFrame.texture:SetColorTexture(0, 0, 0, 1)
    end)
end

frame:SetScript("OnEvent", function(_, event, unit)
    if event == "PLAYER_LOGIN" then
        lastHealth = UnitHealth("player")
        return
    end

    if event == "UNIT_HEALTH" and unit == "player" then
        local health = UnitHealth("player")

        if Sealed(health) then
            secretMode = true
        end

        if secretMode then
            -- Health is a secret value on this build: comparisons throw,
            -- so fall back to flashing on any health change while in combat.
            if InCombatLockdown() then
                Flash()
            end
            return
        end

        local ok, decreased = pcall(function()
            return health < lastHealth
        end)
        if ok and decreased then
            Flash()
        end
        lastHealth = health
    elseif event == "UNIT_SPELLCAST_SUCCEEDED" and unit == "player" then
        -- Covers heals given, attacks performed and spells cast: any
        -- successful cast by the player flashes the pixel.
        Flash()
    end
end)

frame:RegisterEvent("PLAYER_LOGIN")
frame:RegisterUnitEvent("UNIT_HEALTH", "player")
frame:RegisterUnitEvent("UNIT_SPELLCAST_SUCCEEDED", "player")
