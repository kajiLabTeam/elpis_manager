import cv2
import geojson
from pathlib import Path

def main():
    # 1) 画像読み込み
    BASE = Path(__file__).parent
    img_path = BASE / "floor.png"
    img = cv2.imread(str(img_path), cv2.IMREAD_GRAYSCALE)
    if img is None:
        raise FileNotFoundError(f"画像が見つかりません: {img_path}")

    h, w = img.shape
    img_area = h * w

    # 2) 白線（255）だけを2値化 → 輪郭抽出対象マスクを作る
    _, mask = cv2.threshold(img, 200, 255, cv2.THRESH_BINARY)

    # 3) Canny でエッジ強調
    edges = cv2.Canny(mask, 50, 150)

    # 4) 輪郭抽出（RETR_LIST で全輪郭を取りに行く）
    contours, _ = cv2.findContours(edges, cv2.RETR_LIST, cv2.CHAIN_APPROX_SIMPLE)

    features = []
    for i, cnt in enumerate(contours):
        area = cv2.contourArea(cnt)
        # 5) 面積フィルター
        if area < 1000 or area > img_area * 0.9:
            continue

        # 6) 直線近似：ε は輪郭長の 2% 程度
        eps = 0.02 * cv2.arcLength(cnt, True)
        approx = cv2.approxPolyDP(cnt, eps, True)

        # 7) 座標整形 → GeoJSON 形式に
        coords = [[int(pt[0][0]), int(pt[0][1])] for pt in approx]
        if len(coords) < 4:
            continue
        coords.append(coords[0])
        polygon = geojson.Polygon([coords])

        feat = geojson.Feature(
            geometry=polygon,
            properties={
                "id": f"R{i:03}",
                "name": f"Room{i:03}",
                "type": "room",
                "area": int(area)
            }
        )
        features.append(feat)

    # 8) GeoJSON 出力
    fc = geojson.FeatureCollection(features)
    out_path = BASE / "floor_auto.geojson"
    out_path.write_text(geojson.dumps(fc, indent=2), encoding="utf-8")
    print(f"Done: {out_path} ({len(features)} features)")

if __name__ == "__main__":
    main()
