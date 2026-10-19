/* REA ev_c29961641b5e48548c4f096918dd40095a62c188c27b676dd6bdd5ac36e2b728; source response evidence/equipment_changed_handler.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180be90e0(longlong param_1,undefined8 param_2)

{
  longlong *plVar1;
  code *pcVar2;
  longlong lVar3;
  longlong lVar4;
  longlong *plVar5;
  char cVar6;
  
  if (DAT_18507c016 == '\0') {
    FUN_180361910(&DAT_184e0c980);
    FUN_180361910(&DAT_184dc2688);
    DAT_18507c016 = '\x01';
  }
  lVar3 = FUN_182003eb0(param_2,_DAT_184dc2688);
  if (DAT_18507c005 == '\0') {
    FUN_180361910(&DAT_184e1c160);
    DAT_18507c005 = '\x01';
  }
  plVar1 = *(longlong **)(param_1 + 0x10);
  plVar5 = (longlong *)0x0;
  if (plVar1 != (longlong *)0x0) {
    if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
       (plVar5 = plVar1,
       *(longlong *)
        (*(longlong *)(*plVar1 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
       _DAT_184e1c160)) {
      FUN_180360c80(plVar1,_DAT_184e1c160);
      pcVar2 = (code *)swi(3);
      (*pcVar2)();
      return;
    }
  }
  if ((lVar3 != 0) && (plVar5 != (longlong *)0x0)) {
    FUN_180bd9770(plVar5,*(undefined8 *)(lVar3 + 0x20),0);
    if (DAT_18507c005 == '\0') {
      FUN_180361910(&DAT_184e1c160);
      DAT_18507c005 = '\x01';
    }
    cVar6 = DAT_18507c005;
    plVar1 = *(longlong **)(param_1 + 0x10);
    if (plVar1 != (longlong *)0x0) {
      if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
         (*(longlong *)
           (*(longlong *)(*plVar1 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) !=
          _DAT_184e1c160)) {
        FUN_180360c80(plVar1,_DAT_184e1c160);
        pcVar2 = (code *)swi(3);
        (*pcVar2)();
        return;
      }
      if (plVar1[0x32] != 0) {
        *(undefined8 *)(plVar1[0x32] + 0x10) = *(undefined8 *)(lVar3 + 0x28);
        if (cVar6 == '\0') {
          FUN_180361910(&DAT_184e1c160);
          cVar6 = '\x01';
          DAT_18507c005 = '\x01';
        }
        plVar1 = *(longlong **)(param_1 + 0x10);
        if (plVar1 != (longlong *)0x0) {
          if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
             (*(longlong *)
               (*(longlong *)(*plVar1 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8
               ) != _DAT_184e1c160)) {
            FUN_180360c80(plVar1,_DAT_184e1c160);
            pcVar2 = (code *)swi(3);
            (*pcVar2)();
            return;
          }
          *(undefined4 *)(plVar1 + 0x2e) = *(undefined4 *)(lVar3 + 0x30);
          if (cVar6 == '\0') {
            FUN_180361910(&DAT_184e1c160);
            cVar6 = '\x01';
            DAT_18507c005 = '\x01';
          }
          plVar1 = *(longlong **)(param_1 + 0x10);
          if (plVar1 != (longlong *)0x0) {
            if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
               (*(longlong *)
                 (*(longlong *)(*plVar1 + 200) + -8 +
                 (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) != _DAT_184e1c160)) {
              FUN_180360c80(plVar1,_DAT_184e1c160);
              pcVar2 = (code *)swi(3);
              (*pcVar2)();
              return;
            }
            plVar1[0x2f] = *(longlong *)(lVar3 + 0x38);
            if (cVar6 == '\0') {
              FUN_180361910(&DAT_184e1c160);
              DAT_18507c005 = '\x01';
            }
            plVar1 = *(longlong **)(param_1 + 0x10);
            plVar5 = (longlong *)0x0;
            if (plVar1 != (longlong *)0x0) {
              if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
                 (plVar5 = plVar1,
                 *(longlong *)
                  (*(longlong *)(*plVar1 + 200) + -8 +
                  (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) != _DAT_184e1c160)) {
                FUN_180360c80(plVar1);
                pcVar2 = (code *)swi(3);
                (*pcVar2)();
                return;
              }
            }
            if (*(int *)(_DAT_184e0c980 + 0xe4) == 0) {
              il2cpp_runtime_class_init();
            }
            lVar4 = FUN_1808b3070(0);
            cVar6 = DAT_18507c005;
            if (plVar5 != (longlong *)0x0) {
              plVar5[0x30] = lVar4;
              if (cVar6 == '\0') {
                FUN_180361910(&DAT_184e1c160);
                cVar6 = '\x01';
                DAT_18507c005 = '\x01';
              }
              plVar1 = *(longlong **)(param_1 + 0x10);
              if (plVar1 != (longlong *)0x0) {
                if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
                   (*(longlong *)
                     (*(longlong *)(*plVar1 + 200) + -8 +
                     (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) != _DAT_184e1c160)) {
                  FUN_180360c80(plVar1,_DAT_184e1c160);
                  pcVar2 = (code *)swi(3);
                  (*pcVar2)();
                  return;
                }
                *(uint *)(plVar1 + 0x31) = (uint)*(byte *)(lVar3 + 0x40);
                if (cVar6 == '\0') {
                  FUN_180361910(&DAT_184e1c160);
                  DAT_18507c005 = '\x01';
                }
                plVar1 = *(longlong **)(param_1 + 0x10);
                if (plVar1 != (longlong *)0x0) {
                  if ((*(byte *)(*plVar1 + 0x130) < *(byte *)(_DAT_184e1c160 + 0x130)) ||
                     (*(longlong *)
                       (*(longlong *)(*plVar1 + 200) + -8 +
                       (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) != _DAT_184e1c160)) {
                    FUN_180360c80(plVar1,_DAT_184e1c160);
                    pcVar2 = (code *)swi(3);
                    (*pcVar2)();
                    return;
                  }
                  FUN_180bdab90(plVar1,*(undefined8 *)(lVar3 + 0x48),0);
                  if (DAT_18507c005 == '\0') {
                    FUN_180361910(&DAT_184e1c160);
                    DAT_18507c005 = '\x01';
                  }
                  plVar1 = *(longlong **)(param_1 + 0x10);
                  if (plVar1 != (longlong *)0x0) {
                    lVar4 = *plVar1;
                    if ((*(byte *)(_DAT_184e1c160 + 0x130) <= *(byte *)(lVar4 + 0x130)) &&
                       (*(longlong *)
                         (*(longlong *)(lVar4 + 200) + -8 +
                         (ulonglong)*(byte *)(_DAT_184e1c160 + 0x130) * 8) == _DAT_184e1c160)) {
                      (**(code **)(lVar4 + 0x7a8))
                                (plVar1,*(undefined8 *)(lVar3 + 0x18),*(undefined1 *)(lVar3 + 0x50),
                                 *(undefined1 *)(lVar3 + 0x51),*(undefined8 *)(lVar4 + 0x7b0));
                      return;
                    }
                    FUN_180360c80(plVar1,_DAT_184e1c160);
                    pcVar2 = (code *)swi(3);
                    (*pcVar2)();
                    return;
                  }
                }
              }
            }
          }
        }
      }
    }
  }
  FUN_180361b60();
  pcVar2 = (code *)swi(3);
  (*pcVar2)();
  return;
}


