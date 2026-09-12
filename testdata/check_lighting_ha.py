"""Validate generated YAML and scheduling templates in an isolated HA process."""
import asyncio
import json
import sys
from datetime import datetime, timezone

from homeassistant.components.script.config import SCRIPT_ENTITY_SCHEMA
from homeassistant.components.automation.config import PLATFORM_SCHEMA
from homeassistant.components.input_select import CONFIG_SCHEMA
from homeassistant.core import HomeAssistant, Context
from homeassistant.helpers.template import Template
from homeassistant.helpers.script import Script

payload = json.load(sys.stdin)


async def check():
    hass = HomeAssistant("/tmp")
    for config in payload["scripts"].values():
        SCRIPT_ENTITY_SCHEMA(config)
    for config in payload["automations"]:
        PLATFORM_SCHEMA(config)
    CONFIG_SCHEMA({"input_select": payload["helpers"]})
    template = Template(payload["active"], hass)
    base = dict(payload["scripts"]["lighting_assignment_exterior"]["variables"]["assignment"])

    def active(stamp, expected, night=True, **changes):
        assignment = base | changes
        now = datetime.fromisoformat(stamp).replace(tzinfo=timezone.utc)
        result = template.async_render({"assignment": assignment, "now": lambda: now,
                                        "is_state": lambda entity, state: night})
        assert result is expected, (stamp, assignment, result, expected)

    active("2026-12-01T18:00", True)
    active("2026-12-31T23:59", True)
    active("2027-01-01T00:00", False)
    active("2026-12-01T05:00", False)
    active("2026-12-01T18:00", False, night=False)
    active("2026-12-01T23:00", False, off="23:00")
    active("2026-12-01T22:59", True, off="23:00")
    active("2026-12-01T01:59", True, on="22:00", off="02:00")
    active("2026-12-01T02:00", False, on="22:00", off="02:00")
    active("2027-01-05T20:00", True, start="12-15", end="01-05")
    active("2027-01-06T20:00", False, start="12-15", end="01-05")
    active("2028-02-29T20:00", True, start="02-29", end="02-29")
    active("2027-02-28T20:00", False, start="02-29", end="02-29")
    active("2026-12-31T20:00", True, start="2026-12-01", end="2026-12-31")
    active("2027-12-31T20:00", False, start="2026-12-01", end="2026-12-31")
    active("2026-12-31T20:00", False, enabled=False)
    # Verify the actual templated light service data, including native HS import.
    player = payload["scripts"]["lighting_assignment_exterior"]
    data = player["sequence"][2]["repeat"]["sequence"][0]["repeat"]["sequence"][0]["data"]
    step = payload["scripts"]["lighting_sequence_christmas"]["variables"]["sequence_data"]["steps"][0]
    result = Template(data, hass).async_render({"repeat": {"item": step}})
    assert result == {"rgb_color": [255, 0, 0], "brightness": 200, "transition": 0.5}
    step["color"] = {"hs_color": [359, 100]}
    result = Template(data, hass).async_render({"repeat": {"item": step}})
    assert result == {"hs_color": [359, 100], "brightness": 200, "transition": 0.5}
    wled = payload["scripts"]["lighting_assignment_floating_string"]
    assert wled["sequence"][0] == {"action": "light.turn_on", "target": {"entity_id": "light.floating_string"}}
    assert wled["sequence"][1] == {"action": "select.select_option", "target": {"entity_id": "select.floating_string_preset"}, "data": {"option": "Christmas"}}
    assert wled["sequence"][2] == {"wait_template": "{{ false }}"}
    wled_active = template.async_render({"assignment": wled["variables"]["assignment"], "now": lambda: datetime.fromisoformat("2026-12-31T20:00").replace(tzinfo=timezone.utc), "is_state": lambda entity, state: True})
    assert wled_active is True
    # Execute the real HA controller engine using only in-memory mock services.
    calls = []

    async def service(call):
        calls.append((call.domain, call.service, call.data))
        targets = call.data["entity_id"]
        if isinstance(targets, str):
            targets = [targets]
        for entity in targets:
            if call.domain == "script":
                hass.states.async_set(entity, "on" if call.service == "turn_on" else "off")
            elif call.domain == "input_select":
                hass.states.async_set(entity, call.data["option"])

    for domain, services in {"script": ["turn_on", "turn_off"], "light": ["turn_on", "turn_off"], "input_select": ["select_option"]}.items():
        for name in services:
            hass.services.async_register(domain, name, service)
    for id in payload["helpers"]:
        hass.states.async_set("input_select." + id, "idle")
        hass.states.async_set("script." + id, "off")
    hass.states.async_set("sun.sun", "below_horizon")
    config = PLATFORM_SCHEMA(payload["automations"][0])
    controller = Script(hass, config["actions"], "Test controller", "automation")

    async def run(stamp):
        calls.clear()
        await controller.async_run({"now": lambda: datetime.fromisoformat(stamp).replace(tzinfo=timezone.utc)}, context=Context())

    await run("2026-12-31T20:00")
    assert sum(domain == "script" and action == "turn_on" for domain, action, _ in calls) == 3, calls
    await run("2026-12-31T20:01")
    assert not calls, calls  # Do not restart a running sequence each minute.
    hass.states.async_set("input_select.lighting_assignment_exterior", "paused")
    await run("2026-12-31T20:02")
    assert hass.states.is_state("script.lighting_assignment_exterior", "off")
    assert hass.states.is_state("script.lighting_assignment_living_room", "on")
    assert hass.states.is_state("input_select.lighting_assignment_exterior", "paused")
    hass.states.async_set("input_select.lighting_assignment_exterior", "idle")
    await run("2026-12-31T20:03")
    assert hass.states.is_state("script.lighting_assignment_exterior", "on")
    await run("2027-01-01T00:00")
    starts = [i for i, (domain, action, _) in enumerate(calls) if domain == "script" and action == "turn_on"]
    stops = [i for i, (domain, action, _) in enumerate(calls) if domain == "script" and action == "turn_off"]
    assert starts and stops and max(stops) < min(starts), calls
    assert hass.states.is_state("script.lighting_assignment_january", "on")
    # A restart clears script runs, but the HA helper remembers ownership.
    hass.states.async_set("script.lighting_assignment_january", "off")
    await run("2027-02-01T01:00")
    assert any(domain == "light" and action == "turn_off" for domain, action, _ in calls), calls
    assert hass.states.is_state("input_select.lighting_assignment_january", "idle")
    print("HA schemas, WLED actions, 18 template cases and controller lifecycle passed; no live services called.")


asyncio.run(check())
