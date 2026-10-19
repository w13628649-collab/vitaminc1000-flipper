/* REA ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3; source response evidence/new_character_state_application.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180becdc0(longlong param_1,undefined8 param_2)

{
  longlong *plVar1;
  char cVar2;
  longlong lVar3;
  longlong lVar4;
  longlong lVar5;
  longlong *plVar6;
  undefined8 uVar7;
  undefined8 uVar8;
  undefined8 uVar9;
  longlong lVar10;
  code *pcVar11;
  undefined4 uVar12;
  undefined4 uVar13;
  undefined4 uVar14;
  undefined8 local_res10 [2];
  undefined8 local_res20;
  undefined8 in_stack_ffffffffffffff78;
  undefined8 in_stack_ffffffffffffff80;
  undefined4 local_58;
  undefined4 uStack_54;
  undefined4 uStack_50;
  undefined4 uStack_4c;
  
  uVar12 = (undefined4)((ulonglong)in_stack_ffffffffffffff80 >> 0x20);
  uVar14 = (undefined4)((ulonglong)in_stack_ffffffffffffff78 >> 0x20);
  if (DAT_18507c030 == '\0') {
    FUN_180361910(&DAT_184e3ea50);
    FUN_180361910(&DAT_184dc4748);
    FUN_180361910(&DAT_184e540b0);
    FUN_180361910(&DAT_184e57dc0);
    FUN_180361910(&DAT_184e57f18);
    FUN_180361910(&DAT_184e589d8);
    FUN_180361910(&DAT_184e58b30);
    FUN_180361910(&DAT_184db84a8);
    FUN_180361910(&DAT_184dd1e50);
    FUN_180361910(&DAT_184e08f58);
    FUN_180361910(&DAT_184dd6228);
    FUN_180361910(&DAT_184e1df40);
    DAT_18507c030 = '\x01';
  }
  lVar3 = FUN_182003eb0(param_2,_DAT_184dd6228);
  if (lVar3 == 0) goto LAB_180bee7a9;
  if (*(longlong *)(lVar3 + 0x90) != 0) {
    lVar4 = FUN_180360cc0(_DAT_184e3ea50,4);
    lVar5 = FUN_180360cc0(_DAT_184e3ea50,4);
    if (lVar4 == 0) {
LAB_180bee7af:
      FUN_180361b60();
      pcVar11 = (code *)swi(3);
      (*pcVar11)();
      return;
    }
    uVar7 = CONCAT44(uVar14,*(undefined4 *)(lVar4 + 0x18));
    FUN_1831a0ea0(*(undefined8 *)(lVar3 + 0x90),0,lVar4,0,uVar7,0);
    if (lVar5 == 0) goto LAB_180bee7af;
    uVar7 = CONCAT44((int)((ulonglong)uVar7 >> 0x20),*(undefined4 *)(lVar5 + 0x18));
    FUN_1831a0ea0(*(undefined8 *)(lVar3 + 0x90),4,lVar5,0,uVar7,0);
    uVar14 = (undefined4)((ulonglong)uVar7 >> 0x20);
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar6 == 0) goto LAB_180bee7af;
    lVar4 = FUN_1808e2a40(lVar4,*(undefined8 *)(*plVar6 + 0x2d8),0,0);
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar6 == 0) goto LAB_180bee7af;
    lVar5 = FUN_1808e2a40(lVar5,*(undefined8 *)(*plVar6 + 0x2d8),4,0);
    uVar12 = FUN_1830c8fa0(lVar4,0,0);
    uVar13 = FUN_1830c8fa0(lVar5,0,0);
    local_res10[0] = 0;
    FUN_180509120(local_res10,uVar12,uVar13,0);
    *(undefined8 *)(lVar3 + 0x80) = local_res10[0];
    if (lVar4 == 0) goto LAB_180bee7af;
    FUN_1831a0ea0(*(undefined8 *)(lVar3 + 0x98),0,lVar4,0,
                  CONCAT44(uVar14,*(undefined4 *)(lVar4 + 0x18)),0);
    if (lVar5 == 0) goto LAB_180bee7af;
    uVar12 = 0;
    FUN_1831a0ea0(*(undefined8 *)(lVar3 + 0x98),4,lVar5,0,*(undefined4 *)(lVar5 + 0x18),0);
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar6 == 0) goto LAB_180bee7af;
    uVar7 = FUN_1808e2a40(lVar4,*(undefined8 *)(*plVar6 + 0x2d8),0,0);
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (*plVar6 == 0) goto LAB_180bee7af;
    uVar8 = FUN_1808e2a40(lVar5,*(undefined8 *)(*plVar6 + 0x2d8),4,0);
    uVar14 = FUN_1830c8fa0(uVar7,0,0);
    uVar13 = FUN_1830c8fa0(uVar8,0,0);
    local_res20 = 0;
    FUN_180509120(&local_res20,uVar14,uVar13,0);
    *(undefined8 *)(lVar3 + 0x88) = local_res20;
  }
  if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
    il2cpp_runtime_class_init();
  }
  if (DAT_18507a9a2 == '\0') {
    FUN_180361910(&DAT_184e08f58);
    DAT_18507a9a2 = '\x01';
  }
  if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
    il2cpp_runtime_class_init();
  }
  plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
  if (((*plVar6 == 0) || (lVar4 = *(longlong *)(*plVar6 + 0x50), lVar4 == 0)) ||
     (*(longlong *)(lVar4 + 0xa0) == 0)) goto LAB_180bee7a9;
  uVar7 = FUN_180579d90(*(longlong *)(lVar4 + 0xa0),*(undefined4 *)(lVar3 + 0x20),0);
  if (*(longlong *)(lVar4 + 0xa0) == 0) goto LAB_180bee7a9;
  uVar8 = FUN_1805732a0(*(longlong *)(lVar4 + 0xa0),*(undefined4 *)(lVar3 + 0x24),0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x32] == 0) goto LAB_180bee7a9;
  *(undefined8 *)(plVar6[0x32] + 0x10) = *(undefined8 *)(lVar3 + 0x120);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    cVar2 = '\x01';
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 != (longlong *)0x0) {
    if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
       (*(longlong *)
         (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
        _DAT_184e1c160)) {
      FUN_180360c80(plVar6);
      pcVar11 = (code *)swi(3);
      (*pcVar11)();
      return;
    }
  }
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar1 = *(longlong **)(param_1 + 0x10);
  if (plVar1 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar1 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar1);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  uVar9 = FUN_180bd3d60(plVar1,0);
  lVar5 = thunk_FUN_18039ec10(_DAT_184e1df40);
  FUN_180bc6310(lVar5,uVar9,0);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  plVar6[0x3d] = lVar5;
  FUN_180360c40(plVar6 + 0x3d,lVar5);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x3d] == 0) goto LAB_180bee7a9;
  FUN_1807e77e0(plVar6[0x3d],uVar7,uVar8,0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x3d] == 0) goto LAB_180bee7a9;
  FUN_1807e6530(plVar6[0x3d],*(undefined8 *)(lVar3 + 0x30),*(undefined8 *)(lVar3 + 0x38),0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  lVar5 = *plVar6;
  if ((*(byte *)(lVar5 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(lVar5 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  (**(code **)(lVar5 + 0x278))(plVar6,*(undefined8 *)(lVar3 + 0x80),*(undefined8 *)(lVar5 + 0x280));
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  lVar5 = *plVar6;
  if ((*(byte *)(lVar5 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(lVar5 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  (**(code **)(lVar5 + 600))(plVar6,*(undefined4 *)(lVar3 + 0xa8),*(undefined8 *)(lVar5 + 0x260));
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  lVar5 = *plVar6;
  if ((*(byte *)(lVar5 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(lVar5 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  (**(code **)(lVar5 + 0x2d8))(plVar6,*(undefined8 *)(lVar3 + 0xb0),*(undefined8 *)(lVar5 + 0x2e0));
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(undefined4 *)((longlong)plVar6 + 0x2c4) = *(undefined4 *)(lVar3 + 0xac);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x11] == 0) goto LAB_180bee7a9;
  FUN_1828f2880(plVar6[0x11],*(undefined4 *)(lVar3 + 0xb8),*(undefined4 *)(lVar3 + 0xbc),
                *(undefined4 *)(lVar3 + 0xc0),_DAT_184e57f18);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x11] == 0) goto LAB_180bee7a9;
  FUN_1828f21d0(plVar6[0x11],*(undefined8 *)(lVar3 + 200),*(undefined4 *)(lVar3 + 0xc4),
                *(undefined4 *)(lVar3 + 0xb8),_DAT_184e57dc0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x12] == 0) goto LAB_180bee7a9;
  FUN_1828f2880(plVar6[0x12],*(undefined4 *)(lVar3 + 0xd0),*(undefined4 *)(lVar3 + 0xd4),
                *(undefined4 *)(lVar3 + 0xd8),_DAT_184e57f18);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x12] == 0) goto LAB_180bee7a9;
  FUN_1828f21d0(plVar6[0x12],*(undefined8 *)(lVar3 + 0xe0),*(undefined4 *)(lVar3 + 0xdc),
                *(undefined4 *)(lVar3 + 0xd0),_DAT_184e57dc0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x46] == 0) goto LAB_180bee7a9;
  FUN_1828f2880(plVar6[0x46],*(undefined4 *)(lVar3 + 0xe8),*(undefined4 *)(lVar3 + 0xec),
                *(undefined4 *)(lVar3 + 0xf0),_DAT_184e57f18);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if (plVar6[0x46] == 0) goto LAB_180bee7a9;
  FUN_1828f21d0(plVar6[0x46],*(undefined8 *)(lVar3 + 0xf8),*(undefined4 *)(lVar3 + 0xf4),
                *(undefined4 *)(lVar3 + 0xe8),_DAT_184e57dc0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(uint *)(plVar6 + 0x49) = (uint)*(byte *)(lVar3 + 0x110);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  lVar5 = plVar6[0x47];
  uVar14 = *(undefined4 *)(lVar3 + 0x100);
  if (*(char *)(lVar3 + 0x170) == -1) {
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (((*plVar6 == 0) || (lVar10 = *(longlong *)(*plVar6 + 0x50), lVar10 == 0)) ||
       (lVar10 = *(longlong *)(lVar10 + 0x60), lVar10 == 0)) goto LAB_180bee7a9;
    if (*(char *)(lVar10 + 0x648) == '\0') goto LAB_180bedbac;
    uVar13 = 0;
  }
  else {
LAB_180bedbac:
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    if (DAT_18507a9a2 == '\0') {
      FUN_180361910(&DAT_184e08f58);
      DAT_18507a9a2 = '\x01';
    }
    if (*(int *)(_DAT_184e08f58 + 0xe4) == 0) {
      il2cpp_runtime_class_init();
    }
    plVar6 = (longlong *)FUN_180361710(_DAT_184e08f58);
    if (((*plVar6 == 0) || (lVar10 = *(longlong *)(*plVar6 + 0x50), lVar10 == 0)) ||
       (lVar10 = *(longlong *)(lVar10 + 0x60), lVar10 == 0)) goto LAB_180bee7a9;
    uVar13 = *(undefined4 *)(lVar10 + 0x63c);
  }
  if (lVar5 == 0) goto LAB_180bee7a9;
  FUN_1828f2880(lVar5,uVar14,uVar13,0,_DAT_184e58b30);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  lVar5 = plVar6[0x47];
  uVar7 = *(undefined8 *)(lVar3 + 0x108);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  lVar10 = FUN_180bd3dd0(plVar6,0);
  if ((lVar10 == 0) || (lVar5 == 0)) goto LAB_180bee7a9;
  uVar8 = _DAT_184e589d8;
  FUN_1828f21d0(lVar5,uVar7,*(undefined4 *)(lVar10 + 0x30),*(undefined4 *)(lVar3 + 0x100),
                _DAT_184e589d8);
  uVar14 = (undefined4)((ulonglong)uVar8 >> 0x20);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  plVar6[0x41] = *(longlong *)(lVar3 + 0x68);
  FUN_180360c40(plVar6 + 0x41);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(undefined4 *)(plVar6 + 0x42) = *(undefined4 *)(lVar3 + 0x70);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    cVar2 = '\x01';
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(undefined4 *)((longlong)plVar6 + 0x214) = *(undefined4 *)(lVar3 + 0x74);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  plVar6[0x43] = *(longlong *)(lVar3 + 0x78);
  FUN_180360c40(plVar6 + 0x43);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  FUN_180bd9770(plVar6,*(undefined8 *)(lVar3 + 0x118),0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(undefined4 *)(plVar6 + 0x2e) = *(undefined4 *)(lVar3 + 0x128);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  FUN_180bdab90(plVar6,*(undefined8 *)(lVar3 + 0x130),0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  cVar2 = DAT_18507c005;
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  *(undefined1 *)((longlong)plVar6 + 0x16e) = *(undefined1 *)(lVar3 + 0x170);
  if (cVar2 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  FUN_180bd9380(plVar6,*(undefined4 *)(lVar3 + 0x184),0,0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  FUN_180bcd2e0(plVar6,*(undefined1 *)(lVar3 + 0x180),0,0);
  if (*(char *)(lVar3 + 0x188) != '\0') {
    plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
    lVar5 = thunk_FUN_18039ec10(_DAT_184e540b0);
    FUN_1808d5640(lVar5,0);
    lVar10 = FUN_180bf3a50(param_1,0);
    if ((lVar10 == 0) || (lVar5 == 0)) goto LAB_180bee7a9;
    *(undefined8 *)(lVar5 + 0x10) = *(undefined8 *)(lVar10 + 0x10);
    *(undefined4 *)(lVar5 + 0x18) = *(undefined4 *)(lVar3 + 0x18c);
    *(undefined4 *)(lVar5 + 0x1c) = *(undefined4 *)(lVar3 + 400);
    *(undefined1 *)(lVar5 + 0x20) = *(undefined1 *)(lVar3 + 0x194);
    *(undefined1 *)(lVar5 + 0x21) = *(undefined1 *)(lVar3 + 0x195);
    *(undefined4 *)(lVar5 + 0x24) = *(undefined4 *)(lVar3 + 0x198);
    if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
    (**(code **)(*plVar6 + 0x868))(plVar6,lVar5,*(undefined8 *)(*plVar6 + 0x870));
  }
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6,_DAT_184e1c160);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  FUN_180bc5570(plVar6,0,0);
  FUN_180a13540(plVar6,0,0);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
  if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
     (*(longlong *)
       (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
      _DAT_184e1c160)) {
    FUN_180360c80(plVar6);
    pcVar11 = (code *)swi(3);
    (*pcVar11)();
    return;
  }
  if ((plVar6[0x10] == 0) || (lVar5 = *(longlong *)(plVar6[0x10] + 0x40), lVar5 == 0))
  goto LAB_180bee7a9;
  FUN_180918e10(lVar5,lVar3,0);
  cVar2 = *(char *)(lVar3 + 0x171);
  if (cVar2 == '\x01') {
    lVar5 = FUN_180bf3a50(param_1,0);
    if (lVar5 == 0) goto LAB_180bee7a9;
    uVar7 = 7;
LAB_180bee3b7:
    FUN_180a13540(lVar5,uVar7,0);
  }
  else {
    if (cVar2 == '\x02') {
      lVar5 = FUN_180bf3a50(param_1,0);
      if (lVar5 == 0) goto LAB_180bee7a9;
      uVar7 = 0xf;
      goto LAB_180bee3b7;
    }
    if (cVar2 == '\x03') {
      lVar5 = FUN_180bf3a50(param_1,0);
      if (lVar5 == 0) goto LAB_180bee7a9;
      uVar7 = 0x10;
      goto LAB_180bee3b7;
    }
    if (cVar2 == '\x04') {
      lVar5 = FUN_180bf3a50(param_1,0);
      if (lVar5 == 0) goto LAB_180bee7a9;
      uVar7 = 0x13;
      goto LAB_180bee3b7;
    }
  }
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar6 = *(longlong **)(param_1 + 0x10);
  if (plVar6 != (longlong *)0x0) {
    lVar5 = *plVar6;
    if ((*(byte *)(lVar5 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
       (*(longlong *)
         (*(longlong *)(lVar5 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
        _DAT_184e1c160)) {
      FUN_180360c80(plVar6,_DAT_184e1c160);
      pcVar11 = (code *)swi(3);
      (*pcVar11)();
      return;
    }
    local_58 = *(undefined4 *)(lVar3 + 0x58);
    uStack_54 = *(undefined4 *)(lVar3 + 0x5c);
    uStack_50 = *(undefined4 *)(lVar3 + 0x60);
    uStack_4c = *(undefined4 *)(lVar3 + 100);
    (**(code **)(lVar5 + 0x5d8))(plVar6,&local_58,*(undefined8 *)(lVar5 + 0x5e0));
    if (DAT_18507c005 == '\0') {
      FUN_180361910(&DAT_184e1c160);
      DAT_18507c005 = '\x01';
    }
    plVar6 = *(longlong **)(param_1 + 0x10);
    if (plVar6 != (longlong *)0x0) {
      if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
         (*(longlong *)
           (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
          _DAT_184e1c160)) {
        FUN_180360c80(plVar6);
        pcVar11 = (code *)swi(3);
        (*pcVar11)();
        return;
      }
    }
    uVar7 = *(undefined8 *)(lVar3 + 0x178);
    lVar5 = thunk_FUN_18039ec10(_DAT_184db84a8);
    FUN_1807458d0(lVar5,lVar4,uVar7,0);
    if (plVar6 != (longlong *)0x0) {
      plVar6[0x40] = lVar5;
      FUN_180360c40(plVar6 + 0x40,lVar5);
      cVar2 = FUN_182fc26f0(*(undefined8 *)(lVar3 + 0x50),0);
      if (cVar2 == '\0') {
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        (**(code **)(*plVar6 + 0x5f8))
                  (plVar6,*(undefined8 *)(lVar3 + 0x50),*(undefined8 *)(*plVar6 + 0x600));
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        uVar7 = thunk_FUN_18039ec10(_DAT_184dd1e50);
        FUN_180429690(uVar7,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        (**(code **)(*plVar6 + 0x638))(plVar6,uVar7,*(undefined8 *)(*plVar6 + 0x640));
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        lVar5 = (**(code **)(*plVar6 + 0x628))(plVar6,*(undefined8 *)(*plVar6 + 0x630));
        if (lVar5 == 0) goto LAB_180bee7a9;
        FUN_18079ca30(lVar5,*(undefined8 *)(lVar4 + 0x80),*(undefined4 *)(lVar3 + 0x138),
                      *(undefined4 *)(lVar3 + 0x13c),CONCAT44(uVar14,*(undefined4 *)(lVar3 + 0x140))
                      ,CONCAT44(uVar12,*(undefined4 *)(lVar3 + 0x144)),
                      *(undefined4 *)(lVar3 + 0x148),*(undefined4 *)(lVar3 + 0x14c),
                      *(undefined4 *)(lVar3 + 0x150),0);
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        local_58 = *(undefined4 *)(lVar3 + 0x160);
        uStack_54 = *(undefined4 *)(lVar3 + 0x164);
        uStack_50 = *(undefined4 *)(lVar3 + 0x168);
        uStack_4c = *(undefined4 *)(lVar3 + 0x16c);
        (**(code **)(*plVar6 + 0x658))(plVar6,&local_58,*(undefined8 *)(*plVar6 + 0x660));
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        uVar8 = *(undefined8 *)(lVar3 + 0x158);
        pcVar11 = *(code **)(*plVar6 + 0x678);
        uVar7 = *(undefined8 *)(*plVar6 + 0x680);
      }
      else {
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        (**(code **)(*plVar6 + 0x5f8))(plVar6,_DAT_184dc4748,*(undefined8 *)(*plVar6 + 0x600));
        plVar6 = (longlong *)FUN_180bf3a50(param_1,0);
        if (plVar6 == (longlong *)0x0) goto LAB_180bee7a9;
        uVar8 = 0;
        pcVar11 = *(code **)(*plVar6 + 0x638);
        uVar7 = *(undefined8 *)(*plVar6 + 0x640);
      }
      (*pcVar11)(plVar6,uVar8,uVar7);
      if (DAT_18507c005 == '\0') {
        FUN_180361910(&DAT_184e1c160);
        DAT_18507c005 = '\x01';
      }
      cVar2 = DAT_18507c005;
      plVar6 = *(longlong **)(param_1 + 0x10);
      if (plVar6 != (longlong *)0x0) {
        if ((*(byte *)(*plVar6 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
           (*(longlong *)
             (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8)
            != _DAT_184e1c160)) {
          FUN_180360c80(plVar6,_DAT_184e1c160);
          pcVar11 = (code *)swi(3);
          (*pcVar11)();
          return;
        }
        uVar14 = *(undefined4 *)(lVar3 + 0x44);
        uVar12 = *(undefined4 *)(lVar3 + 0x48);
        uVar13 = *(undefined4 *)(lVar3 + 0x4c);
        *(undefined4 *)(plVar6 + 0x3e) = *(undefined4 *)(lVar3 + 0x40);
        *(undefined4 *)((longlong)plVar6 + 500) = uVar14;
        *(undefined4 *)(plVar6 + 0x3f) = uVar12;
        *(undefined4 *)((longlong)plVar6 + 0x1fc) = uVar13;
        if (cVar2 == '\0') {
          FUN_180361910(&DAT_184e1c160);
          DAT_18507c005 = '\x01';
        }
        plVar6 = *(longlong **)(param_1 + 0x10);
        if (plVar6 != (longlong *)0x0) {
          if ((*(byte *)(_DAT_184e1c160 + 0x130) <= *(byte *)(*plVar6 + 0x130)) &&
             (*(longlong *)
               (*(longlong *)(*plVar6 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8
               ) == _DAT_184e1c160)) {
            FUN_180bd18f0(plVar6,0);
            return;
          }
          FUN_180360c80(plVar6);
          pcVar11 = (code *)swi(3);
          (*pcVar11)();
          return;
        }
      }
    }
  }
LAB_180bee7a9:
  FUN_180361b60();
  pcVar11 = (code *)swi(3);
  (*pcVar11)();
  return;
}


