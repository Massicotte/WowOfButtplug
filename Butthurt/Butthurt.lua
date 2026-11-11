-- Butthurt - WoW: Forever port with event types and intensity
-- The combat log is not available to addons on the Forever client, so
-- damage and healing are detected through UNIT_HEALTH changes instead.
-- Events are drawn as a colored 5x5 block (event type) next to a gray
-- 5x5 block (intensity 0-1) in the top-left corner; the native bridge
-- reads them off the screen.

local FLASH_DURATION = 0.3

local COLOR_FOR_EVENT = {
    damage  = {1, 0, 0},
    heal    = {0, 1, 0},
    loot    = {1, 1, 0},
    mining  = {0, 1, 1},
    whisper = {1, 0, 1},
    guild   = {0, 0, 1},
}

local EVENT_LABEL = {
    damage  = "Degats",
    heal    = "Soin",
    loot    = "Coffre / butin",
    mining  = "Minage",
    whisper = "Message prive",
    guild   = "Message de guilde",
}

local EVENT_ORDER = {"damage", "heal", "loot", "mining", "whisper", "guild"}

local MINING_SPELLS = {["Mining"] = true, ["Minage"] = true}

local defaults = {
    damage  = true,
    heal    = true,
    loot    = true,
    mining  = true,
    whisper = true,
    guild   = true,
}

local frame = CreateFrame("Frame")
local pixelFlashFrame = CreateFrame("Frame", "PixelFlashFrame", UIParent)
pixelFlashFrame:SetFrameStrata("HIGH")
pixelFlashFrame:SetWidth(10)
pixelFlashFrame:SetHeight(5)
pixelFlashFrame:SetPoint("TOPLEFT", UIParent, "TOPLEFT")
pixelFlashFrame.eventTex = pixelFlashFrame:CreateTexture(nil, "BACKGROUND")
pixelFlashFrame.eventTex:SetSize(5, 5)
pixelFlashFrame.eventTex:SetPoint("TOPLEFT", pixelFlashFrame, "TOPLEFT")
pixelFlashFrame.eventTex:SetColorTexture(0, 0, 0, 1)
pixelFlashFrame.intensityTex = pixelFlashFrame:CreateTexture(nil, "BACKGROUND")
pixelFlashFrame.intensityTex:SetSize(5, 5)
pixelFlashFrame.intensityTex:SetPoint("TOPLEFT", pixelFlashFrame, "TOPLEFT", 5, 0)
pixelFlashFrame.intensityTex:SetColorTexture(0, 0, 0, 1)

local DB
local lastHealth
local secretMode = false
local lastMining = 0

local function Sealed(value)
    return type(issecretvalue) == "function" and issecretvalue(value)
end

local function Flash(event, intensity)
    if not DB[event] then return end
    local color = COLOR_FOR_EVENT[event]
    intensity = math.min(math.max(intensity or 0.5, 0), 1)
    pixelFlashFrame.eventTex:SetColorTexture(color[1], color[2], color[3], 1)
    pixelFlashFrame.intensityTex:SetColorTexture(intensity, intensity, intensity, 1)
    C_Timer.After(FLASH_DURATION, function()
        pixelFlashFrame.eventTex:SetColorTexture(0, 0, 0, 1)
        pixelFlashFrame.intensityTex:SetColorTexture(0, 0, 0, 1)
    end)
end

local function OnUnitHealth()
    local health = UnitHealth("player")
    if Sealed(health) then
        secretMode = true
    end
    if secretMode then
        if InCombatLockdown() then
            Flash("damage", 0.5)
        end
        return
    end

    if lastHealth == nil then
        lastHealth = health
        return
    end

    local ok, changed, up, intensity
    ok, changed = pcall(function() return health ~= lastHealth end)
    if not ok or not changed then
        lastHealth = health
        return
    end
    ok, up = pcall(function() return health > lastHealth end)
    ok, intensity = pcall(function()
        local maxHP = UnitHealthMax("player")
        if maxHP <= 0 then return 0.5 end
        if up then
            return (health - lastHealth) / maxHP
        else
            return (lastHealth - health) / maxHP
        end
    end)

    if up then
        Flash("heal", ok and intensity or 0.5)
    else
        Flash("damage", ok and intensity or 0.5)
    end
    lastHealth = health
end

local function OnSpellSucceeded(unit, _, spellID)
    if unit ~= "player" then return end
    local name
    if C_Spell and C_Spell.GetSpellInfo then
        local info = C_Spell.GetSpellInfo(spellID)
        name = info and info.name or nil
    end
    if not name and GetSpellInfo then
        name = GetSpellInfo(spellID)
    end
    if name and MINING_SPELLS[name] then
        lastMining = GetTime()
        Flash("mining", 0.5)
    end
end

frame:SetScript("OnEvent", function(_, event, ...)
    if event == "PLAYER_LOGIN" then
        DB = ButthurtSavedVariables or {}
        for key, value in pairs(defaults) do
            if DB[key] == nil then DB[key] = value end
        end
        ButthurtSavedVariables = DB
        lastHealth = UnitHealth("player")
        return
    end

    if event == "UNIT_HEALTH" then
        OnUnitHealth()
    elseif event == "UNIT_SPELLCAST_SUCCEEDED" then
        OnSpellSucceeded(...)
    elseif event == "LOOT_OPENED" then
        if GetTime() - lastMining < 2 then return end
        Flash("loot", 0.5)
    elseif event == "CHAT_MSG_WHISPER" then
        Flash("whisper", 1.0)
    elseif event == "CHAT_MSG_GUILD" then
        Flash("guild", 0.5)
    end
end)

frame:RegisterEvent("PLAYER_LOGIN")
frame:RegisterUnitEvent("UNIT_HEALTH", "player")
frame:RegisterUnitEvent("UNIT_SPELLCAST_SUCCEEDED", "player")
frame:RegisterEvent("LOOT_OPENED")
frame:RegisterEvent("CHAT_MSG_WHISPER")
frame:RegisterEvent("CHAT_MSG_GUILD")

-- ------------------------------------------------------- config window ----

local panel = CreateFrame("Frame", "ButthurtConfigPanel", UIParent, "BasicFrameTemplateWithInset")
panel:SetSize(240, 240)
panel:SetPoint("CENTER")
panel:SetMovable(true)
panel:EnableMouse(true)
panel:RegisterForDrag("LeftButton")
panel:SetScript("OnDragStart", panel.StartMoving)
panel:SetScript("OnDragStop", panel.StopMovingOrSizing)
panel:SetFrameStrata("DIALOG")
panel:Hide()

panel.title = panel:CreateFontString(nil, "OVERLAY", "GameFontNormal")
panel.title:SetPoint("TOP", 0, -5)
panel.title:SetText("Butthurt - Vibrations")

local checkboxes = {}
for i, event in ipairs(EVENT_ORDER) do
    local cb = CreateFrame("CheckButton", "ButthurtCheck_" .. event, panel, "UICheckButtonTemplate")
    cb:SetPoint("TOPLEFT", 20, -30 - (i - 1) * 28)
    cb.Text:SetText(EVENT_LABEL[event])
    cb:SetChecked(DB and DB[event] or true)
    cb:SetScript("OnClick", function(self)
        DB[event] = self:GetChecked() or false
    end)
    cb.event = event
    checkboxes[event] = cb
end

SLASH_BUTTHURT1 = "/butthurt"
SLASH_WOB1 = "/wob"
SlashCmdList.BUTTHURT = function()
    if DB then
        for _, cb in pairs(checkboxes) do
            cb:SetChecked(DB[cb.event])
        end
    end
    if panel:IsShown() then
        panel:Hide()
    else
        panel:Show()
    end
end
SlashCmdList.WOB = SlashCmdList.BUTTHURT
