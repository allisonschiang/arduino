"""
End-to-end test for the viam:arduino:uno-q board over the Viam SDK.

Setup:
  pip install viam-sdk
  Fill in API_KEY / API_KEY_ID / ADDRESS below (app.viam.com -> your machine ->
  CONNECT tab -> Code sample -> Python shows all three).

Wiring (2-jumper loopback):
  D13 -> D2    (interrupt: toggling D13 makes edges on pin 2 = int2)
  D9  -> A0    (analog/PWM: D9 drives, A0 reads)

Config the board component attributes as:
  {"analogs":[{"name":"a0","pin":"0"}],
   "digital_interrupts":[{"name":"int2","pin":"2","mode":"CHANGE"}]}

Run:  python test_board.py
"""

import asyncio

from viam.robot.client import RobotClient
from viam.components.board import Board

API_KEY = "<API_KEY>"
API_KEY_ID = "<API_KEY_ID>"
ADDRESS = "<ADDRESS>"  # e.g. my-machine-main.abcd1234.viam.cloud


async def connect() -> RobotClient:
    opts = RobotClient.Options.with_api_key(api_key=API_KEY, api_key_id=API_KEY_ID)
    return await RobotClient.at_address(ADDRESS, opts)


async def main() -> None:
    robot = await connect()
    board = Board.from_robot(robot, "board")

    # ---- Digital interrupts (the main test) --------------------------------
    # D13 is jumpered to D2. In CHANGE mode each full toggle (high->low) is TWO
    # edges, so 10 toggles should add ~20 ticks.
    di = await board.digital_interrupt_by_name("int2")
    pin13 = await board.gpio_pin_by_name("13")

    before = await di.value()
    print(f"int2 before: {before}")

    for _ in range(10):
        await pin13.set(True)
        await asyncio.sleep(0.03)
        await pin13.set(False)
        await asyncio.sleep(0.03)

    after = await di.value()
    print(f"int2 after:  {after}")
    print(f"delta: {after - before}  (expect ~20 in CHANGE mode; >0 means interrupts work)")

    # ---- Analog (D9 drives A0) --------------------------------------------
    pin9 = await board.gpio_pin_by_name("9")
    a0 = await board.analog_by_name("a0")

    await pin9.set(True)
    await asyncio.sleep(0.05)
    hi = await a0.read()
    await pin9.set(False)
    await asyncio.sleep(0.05)
    lo = await a0.read()
    print(f"analog: D9 high -> a0={hi.value} (~4095), D9 low -> a0={lo.value} (~0)")

    # ---- PWM (D9 -> A0, noisy without an RC filter) -----------------------
    await pin9.set_pwm(0.5)
    await asyncio.sleep(0.05)
    samples = [(await a0.read()).value for _ in range(6)]
    print(f"pwm: D9 duty=0.5 -> a0 samples {samples} (jumping/mid = PWM active)")
    await pin9.set_pwm_frequency(1000)  # accept-only; measure Hz with a scope/buzzer
    await pin9.set_pwm(0.0)

    await robot.close()


if __name__ == "__main__":
    asyncio.run(main())
