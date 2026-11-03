# Arduino / ESP32 LED rig

This rig turns a Crosswire `morse-led` or `binary-toggle` pulse timeline into physical
LED pulses, for testing an agent that reads a light sensor or a camera. The
`crosswire bridge` serial backend accepts only the configured device ID and pin,
validates every step and duration against the allowlist, and always sends a stop.

1. Install ArduinoJson, open `crosswire_led_rig/crosswire_led_rig.ino`, set `LED_PIN`
   to the pin you will allowlist, and flash it to the Arduino or ESP32.

2. Build Crosswire and start the bridge on that serial port:

   ```bash
   go build -o crosswire ./cmd/crosswire
   ./crosswire bridge --kind led --serial-port /dev/tty.usbmodem0001 \
     --device-id arduino-01 --pin 9
   ```

   `./crosswire bridge --simulate --kind led` drives nothing and lets you check the
   target profile without hardware.

3. In **Targets**, create a target with `transport: bridge` and:

   - action URL: `http://127.0.0.1:9910/action`
   - stop URL: `http://127.0.0.1:9910/stop`
   - kind: `led`
   - device ID: `arduino-01`
   - allowed resource: `pin-9`

4. In **Repeater**, pick the `morse-led` or `binary-toggle` adapter. Set an
   **observer** on the target (an HTTP state endpoint, or a page read in headless
   Chrome) so the agent's reaction becomes evidence under the nonce and control rules.

Keep the LED within the board's current limits. Use a resistor, and an external driver
when the light source needs more current than the GPIO can safely supply.

The wire protocol between the bridge and the board is in
[`docs/CONTRACT.md`](../../docs/CONTRACT.md) section 10.1.
