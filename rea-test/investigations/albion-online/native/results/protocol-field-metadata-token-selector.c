/* REA ev_8b515996b4f5c2c0db1f572420b0f4397a9ef2d06b89027aea28c320205bbb08; source response evidence/protocol-field-metadata-token-selector.json; static pseudocode. */

void FUN_18084b3a0(undefined8 param_1,longlong *param_2)

{
  code *pcVar1;
  
  if (param_2 != (longlong *)0x0) {
                    /* WARNING: Could not recover jumptable at 0x00018084b3c4. Too many branches */
                    /* WARNING: Treating indirect jump as call */
    (**(code **)(*param_2 + 0x228))(param_2,*(undefined8 *)(*param_2 + 0x230));
    return;
  }
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}


