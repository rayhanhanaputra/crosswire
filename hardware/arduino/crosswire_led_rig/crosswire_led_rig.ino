#include <ArduinoJson.h>

// Change this constant to the exact pin allowlisted in the Crosswire bridge rig profile.
constexpr int LED_PIN = 9;
constexpr size_t MAX_STEPS = 256;
constexpr unsigned long MAX_WATCHDOG_MS = 30000;

struct Step {
  bool on;
  uint8_t units;
};

Step steps[MAX_STEPS];
size_t stepCount = 0;
size_t stepIndex = 0;
unsigned long unitMs = 200;
unsigned long stepStartedAt = 0;
unsigned long sequenceStartedAt = 0;
unsigned long watchdogMs = MAX_WATCHDOG_MS;
bool sequenceActive = false;

void stopSequence() {
  sequenceActive = false;
  stepCount = 0;
  stepIndex = 0;
  digitalWrite(LED_PIN, LOW);
}

void reply(const char* status, const char* detail) {
  JsonDocument response;
  response["status"] = status;
  response["detail"] = detail;
  serializeJson(response, Serial);
  Serial.println();
}

void handleCommand(const String& line) {
  JsonDocument command;
  DeserializationError error = deserializeJson(command, line);
  if (error) {
    reply("error", "invalid_json");
    return;
  }
  int pin = command["pin"] | -1;
  if (pin != LED_PIN) {
    reply("error", "pin_not_allowlisted");
    return;
  }
  const char* name = command["command"] | "";
  if (strcmp(name, "stop") == 0) {
    stopSequence();
    reply("ok", "stopped");
    return;
  }
  if (strcmp(name, "led_sequence") != 0) {
    reply("error", "unsupported_command");
    return;
  }
  JsonArray incoming = command["steps"].as<JsonArray>();
  if (incoming.isNull() || incoming.size() == 0 || incoming.size() > MAX_STEPS) {
    reply("error", "invalid_steps");
    return;
  }
  unsigned long requestedUnit = command["unit_ms"] | 0;
  unsigned long requestedWatchdog = command["watchdog_ms"] | 0;
  if (requestedUnit < 50 || requestedUnit > 2000 || requestedWatchdog == 0 ||
      requestedWatchdog > MAX_WATCHDOG_MS) {
    reply("error", "invalid_limits");
    return;
  }
  size_t index = 0;
  for (JsonObject item : incoming) {
    const char* state = item["state"] | "";
    int units = item["units"] | 0;
    if ((strcmp(state, "on") != 0 && strcmp(state, "off") != 0) ||
        units < 1 || units > 20) {
      stopSequence();
      reply("error", "invalid_step");
      return;
    }
    steps[index++] = {strcmp(state, "on") == 0, static_cast<uint8_t>(units)};
  }
  stopSequence();
  stepCount = index;
  unitMs = requestedUnit;
  watchdogMs = requestedWatchdog;
  stepStartedAt = millis();
  sequenceStartedAt = stepStartedAt;
  sequenceActive = true;
  digitalWrite(LED_PIN, steps[0].on ? HIGH : LOW);
  reply("ok", "accepted");
}

void setup() {
  pinMode(LED_PIN, OUTPUT);
  digitalWrite(LED_PIN, LOW);
  Serial.begin(115200);
}

void loop() {
  if (Serial.available()) {
    String line = Serial.readStringUntil('\n');
    line.trim();
    if (line.length() > 0) handleCommand(line);
  }
  if (!sequenceActive) return;
  unsigned long now = millis();
  if (now - sequenceStartedAt >= watchdogMs) {
    stopSequence();
    return;
  }
  if (now - stepStartedAt < steps[stepIndex].units * unitMs) return;
  stepIndex++;
  if (stepIndex >= stepCount) {
    stopSequence();
    return;
  }
  stepStartedAt = now;
  digitalWrite(LED_PIN, steps[stepIndex].on ? HIGH : LOW);
}
